package policy

import "testing"

// TestDecide usa "table-driven tests": una tabla de casos, un solo bucle que
// los recorre. Cada caso describe un Snapshot de entrada y la Accion que se
// espera de vuelta. Cubre los casos límite relevantes de la política CPU-only.
func TestDecide(t *testing.T) {
	casos := []struct {
		nombre   string
		entrada  Snapshot
		esperada Accion
	}{
		// --- Guardas de seguridad (van antes que cualquier umbral) ---
		{
			nombre:   "metrica no confiable con CPU alta: mantiene",
			entrada:  Snapshot{InstanciasCorriendo: 2, CPUMax: 95, MetricaConfiable: false},
			esperada: Mantener,
		},
		{
			nombre:   "en cooldown con CPU alta: mantiene",
			entrada:  Snapshot{InstanciasCorriendo: 2, CPUMax: 95, MetricaConfiable: true, EnCooldown: true},
			esperada: Mantener,
		},
		{
			nombre:   "en cooldown con CPU baja: mantiene",
			entrada:  Snapshot{InstanciasCorriendo: 3, CPUMax: 5, MetricaConfiable: true, EnCooldown: true},
			esperada: Mantener,
		},

		// --- Subir ---
		{
			nombre:   "CPU muy alta con margen: sube",
			entrada:  Snapshot{InstanciasCorriendo: 2, CPUMax: 85, MetricaConfiable: true},
			esperada: Subir,
		},
		{
			nombre:   "CPU justo por encima del umbral (70.01): sube",
			entrada:  Snapshot{InstanciasCorriendo: 1, CPUMax: 70.01, MetricaConfiable: true},
			esperada: Subir,
		},
		{
			nombre:   "CPU exactamente en el umbral de subida (70.0): mantiene",
			entrada:  Snapshot{InstanciasCorriendo: 1, CPUMax: 70.0, MetricaConfiable: true},
			esperada: Mantener,
		},
		{
			nombre:   "CPU alta pero ya en el maximo de instancias: mantiene",
			entrada:  Snapshot{InstanciasCorriendo: 5, CPUMax: 99, MetricaConfiable: true},
			esperada: Mantener,
		},

		// --- Bajar ---
		{
			nombre:   "CPU muy baja con instancias de sobra: baja",
			entrada:  Snapshot{InstanciasCorriendo: 3, CPUMax: 10, MetricaConfiable: true},
			esperada: Bajar,
		},
		{
			nombre:   "CPU justo por debajo del umbral (29.99): baja",
			entrada:  Snapshot{InstanciasCorriendo: 2, CPUMax: 29.99, MetricaConfiable: true},
			esperada: Bajar,
		},
		{
			nombre:   "CPU exactamente en el umbral de bajada (30.0): mantiene",
			entrada:  Snapshot{InstanciasCorriendo: 2, CPUMax: 30.0, MetricaConfiable: true},
			esperada: Mantener,
		},
		{
			nombre:   "CPU baja pero ya en el minimo de instancias: mantiene",
			entrada:  Snapshot{InstanciasCorriendo: 1, CPUMax: 5, MetricaConfiable: true},
			esperada: Mantener,
		},

		// --- Banda intermedia (just-in-need) ---
		{
			nombre:   "CPU en banda normal (50): mantiene",
			entrada:  Snapshot{InstanciasCorriendo: 2, CPUMax: 50, MetricaConfiable: true},
			esperada: Mantener,
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			got := Decide(c.entrada)
			if got.Accion != c.esperada {
				t.Errorf("Decide() = %v (motivo: %q); se esperaba %v",
					got.Accion, got.Motivo, c.esperada)
			}
			if got.Motivo == "" {
				t.Errorf("Decide() devolvio un motivo vacio; toda decision debe ser explicable")
			}
		})
	}
}
