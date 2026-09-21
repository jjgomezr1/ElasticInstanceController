package policy

import "testing"

// TestDecide cubre la política final (CPU + latencia + hosts saludables) con
// table-driven tests. Umbrales: CPU 60/30, latencia 1.0/0.4.
func TestDecide(t *testing.T) {
	// Snapshot base "sano" que por defecto lleva a mantener; cada caso ajusta
	// solo los campos que le interesan.
	base := func() Snapshot {
		return Snapshot{
			InstanciasCorriendo: 2,
			CPUMax:              45, // banda normal
			LatenciaAvg:         0.5,
			HostsSaludables:     2,
			MetricaConfiable:    true,
			EnCooldown:          false,
		}
	}

	casos := []struct {
		nombre   string
		ajustar  func(s *Snapshot)
		esperada Accion
	}{
		// --- Garantía del mínimo (prioridad máxima) ---
		{"cero instancias con metrica no confiable: sube (garantiza minimo)",
			func(s *Snapshot) { s.InstanciasCorriendo = 0; s.MetricaConfiable = false }, Subir},
		{"cero instancias en cooldown: sube igual (ignora cooldown)",
			func(s *Snapshot) { s.InstanciasCorriendo = 0; s.MetricaConfiable = false; s.EnCooldown = true }, Subir},
		{"cero instancias con todo en cero: sube",
			func(s *Snapshot) { s.InstanciasCorriendo = 0; s.CPUMax = 0; s.LatenciaAvg = 0; s.HostsSaludables = 0; s.MetricaConfiable = false }, Subir},

		// --- Guardas de seguridad ---
		{"metrica no confiable con CPU alta: mantiene",
			func(s *Snapshot) { s.CPUMax = 95; s.MetricaConfiable = false }, Mantener},
		{"en cooldown con CPU alta: mantiene",
			func(s *Snapshot) { s.CPUMax = 95; s.EnCooldown = true }, Mantener},

		// --- Subir por CPU ---
		{"CPU por encima de 60 con margen: sube",
			func(s *Snapshot) { s.CPUMax = 75 }, Subir},
		{"CPU justo sobre el umbral (60.01): sube",
			func(s *Snapshot) { s.CPUMax = 60.01 }, Subir},
		{"CPU exactamente en 60: mantiene (umbral estricto)",
			func(s *Snapshot) { s.CPUMax = 60.0 }, Mantener},
		{"CPU alta pero ya en el maximo: mantiene",
			func(s *Snapshot) { s.CPUMax = 95; s.InstanciasCorriendo = 5; s.HostsSaludables = 5 }, Mantener},

		// --- Subir por LATENCIA (aunque CPU esté baja) ---
		{"latencia alta con CPU baja: sube",
			func(s *Snapshot) { s.CPUMax = 10; s.LatenciaAvg = 2.0 }, Subir},
		{"latencia justo sobre 1s: sube",
			func(s *Snapshot) { s.LatenciaAvg = 1.01 }, Subir},
		{"latencia exactamente 1s: no sube por latencia",
			func(s *Snapshot) { s.CPUMax = 45; s.LatenciaAvg = 1.0 }, Mantener},

		// --- Bajar ---
		{"CPU y latencia bajas, hosts sanos, con margen: baja",
			func(s *Snapshot) { s.CPUMax = 10; s.LatenciaAvg = 0.2; s.InstanciasCorriendo = 3; s.HostsSaludables = 3 }, Bajar},
		{"carga baja pero ya en el minimo: mantiene",
			func(s *Snapshot) { s.CPUMax = 10; s.LatenciaAvg = 0.2; s.InstanciasCorriendo = 1; s.HostsSaludables = 1 }, Mantener},
		{"carga baja pero un host no sano: mantiene",
			func(s *Snapshot) { s.CPUMax = 10; s.LatenciaAvg = 0.2; s.InstanciasCorriendo = 3; s.HostsSaludables = 2 }, Mantener},
		{"CPU baja pero latencia en banda muerta (0.5): mantiene",
			func(s *Snapshot) { s.CPUMax = 10; s.LatenciaAvg = 0.5; s.InstanciasCorriendo = 3; s.HostsSaludables = 3 }, Mantener},
		{"CPU baja pero latencia justo en 0.4: mantiene (umbral estricto)",
			func(s *Snapshot) { s.CPUMax = 10; s.LatenciaAvg = 0.4; s.InstanciasCorriendo = 3; s.HostsSaludables = 3 }, Mantener},

		// --- Banda intermedia ---
		{"CPU y latencia en banda normal: mantiene",
			func(s *Snapshot) {}, Mantener},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			s := base()
			c.ajustar(&s)
			got := Decide(s)
			if got.Accion != c.esperada {
				t.Errorf("Decide() = %v (motivo: %q); se esperaba %v", got.Accion, got.Motivo, c.esperada)
			}
			if got.Motivo == "" {
				t.Errorf("Decide() devolvio motivo vacio; toda decision debe ser explicable")
			}
		})
	}
}
