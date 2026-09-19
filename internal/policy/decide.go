// Package policy contiene la lógica de decisión del controlador. Es un
// paquete PURO: no importa nada de AWS, no lee el reloj, no imprime ni
// registra. Recibe una foto del estado observado (Snapshot) y devuelve una
// decisión (Decision). Por eso se puede probar de forma exhaustiva y
// determinista con `go test`, sin tocar la infraestructura real.
package policy

import "autoscaling-controller/internal/config"

// Accion es el resultado categórico de una decisión: una de tres palabras.
type Accion int

const (
	// Mantener: no cambiar la cantidad de instancias, seguir observando.
	Mantener Accion = iota
	// Subir: lanzar una instancia nueva.
	Subir
	// Bajar: terminar una instancia existente.
	Bajar
)

// String hace que una Accion se imprima con su nombre legible en vez de un
// número, lo cual es útil para logs y para los mensajes de los tests.
func (a Accion) String() string {
	switch a {
	case Subir:
		return "subir"
	case Bajar:
		return "bajar"
	default:
		return "mantener"
	}
}

// Snapshot es la foto del estado observado en un ciclo. Es SOLO datos: quien
// lo construye (las piezas que hablan con AWS) llena estos campos, y Decide()
// los interpreta.
type Snapshot struct {
	// InstanciasCorriendo es cuántas instancias gestionadas están "running"
	// según el inventario (Pieza 1).
	InstanciasCorriendo int

	// CPUMax es el máximo, entre las instancias, del promedio de CPU de cada
	// una durante la ventana de observación (Pieza 2). En porcentaje 0-100.
	CPUMax float64

	// MetricaConfiable indica si el dato de CPU es lo bastante reciente como
	// para confiar en él. Si es false, Decide() mantiene el estado actual sin
	// arriesgar una decisión con datos obsoletos.
	MetricaConfiable bool

	// EnCooldown indica si todavía estamos dentro del periodo de enfriamiento
	// posterior a una acción previa de escalado. Si es true, no se decide
	// subir ni bajar (se deja que el tráfico se reparta y las métricas se
	// estabilicen).
	EnCooldown bool

	// --- Campos de ALB: reservados para una fase futura. ---
	// Hoy NO se usan en Decide() porque no existe el Load Balancer todavía.
	// Se dejan presentes para que agregar la lógica de ALB más adelante no
	// obligue a cambiar la firma de Decide() ni de quienes construyen el
	// Snapshot.
	LatenciaAvg     float64 // TargetResponseTime (segundos) — sin uso aún.
	Errores5xx      int     // HTTPCode_Target_5XX_Count — sin uso aún.
	HostsSaludables int     // HealthyHostCount — sin uso aún.
}

// Decision es lo que Decide() devuelve: la acción a tomar y una justificación
// en texto para que cada decisión sea explicable a partir del estado observado.
type Decision struct {
	Accion Accion
	Motivo string
}

// Decide es el corazón de la política. Dada una foto del estado, decide si
// mantener, subir o bajar. En esta fase la CPU es la ÚNICA métrica.
//
// Orden de evaluación (importa: las guardas de seguridad van primero):
//  1. Si la métrica no es confiable -> mantener.
//  2. Si estamos en cooldown -> mantener.
//  3. Subir: CPU por encima del umbral Y aún hay margen para más instancias.
//  4. Bajar: CPU por debajo del umbral Y hay más de una instancia.
//  5. En cualquier otro caso -> mantener.
func Decide(s Snapshot) Decision {
	// 1. Datos no confiables: no decidir en falso.
	if !s.MetricaConfiable {
		return Decision{
			Accion: Mantener,
			Motivo: "metrica de CPU no confiable (obsoleta o faltante); se mantiene el estado actual",
		}
	}

	// 2. Cooldown activo: dar tiempo a que el efecto de la acción previa se
	// asiente antes de tomar otra.
	if s.EnCooldown {
		return Decision{
			Accion: Mantener,
			Motivo: "en cooldown tras una accion previa; se mantiene el estado actual",
		}
	}

	// 3. ¿Subir? Requiere CPU alta y no haber llegado al tope de instancias.
	if s.CPUMax > config.CPUSubir {
		if s.InstanciasCorriendo >= config.MaxInstancias {
			return Decision{
				Accion: Mantener,
				Motivo: "CPU alta pero ya se alcanzo el maximo de instancias; se mantiene",
			}
		}
		return Decision{
			Accion: Subir,
			Motivo: "CPU maxima por encima del umbral de subida con margen de capacidad disponible",
		}
	}

	// 4. ¿Bajar? Requiere CPU baja y tener más de la instancia mínima.
	if s.CPUMax < config.CPUBajar {
		if s.InstanciasCorriendo <= config.MinInstancias {
			return Decision{
				Accion: Mantener,
				Motivo: "CPU baja pero ya se esta en el minimo de instancias; se mantiene",
			}
		}
		return Decision{
			Accion: Bajar,
			Motivo: "CPU maxima por debajo del umbral de bajada con instancias de sobra",
		}
	}

	// 5. Zona intermedia (entre los umbrales): estado "just-in-need", no se
	// toca la capacidad.
	return Decision{
		Accion: Mantener,
		Motivo: "CPU dentro de la banda normal; capacidad adecuada, se mantiene",
	}
}
