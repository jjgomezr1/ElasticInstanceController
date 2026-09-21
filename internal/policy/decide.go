// Package policy contiene la lógica de decisión del controlador. Es un
// paquete PURO: no importa nada de AWS, no lee el reloj, no imprime ni
// registra. Recibe una foto del estado observado (Snapshot) y devuelve una
// decisión (Decision). Por eso se puede probar de forma exhaustiva y
// determinista con `go test`, sin tocar la infraestructura real.
package policy

import (
	"fmt"

	"autoscaling-controller/internal/config"
)

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

	// --- Métricas del ALB (fase ALB) ---

	// LatenciaAvg es el TargetResponseTime del ALB (segundos): cuánto tarda el
	// target en responder. Señal de experiencia de usuario.
	LatenciaAvg float64

	// HostsSaludables es el HealthyHostCount del Target Group: cuántas
	// instancias están sanas según el health check del ALB. Se usa como guarda
	// de seguridad para bajar (no reducir si alguna no está sana).
	HostsSaludables int
}

// Decision es lo que Decide() devuelve: la acción a tomar y una justificación
// en texto para que cada decisión sea explicable a partir del estado observado.
type Decision struct {
	Accion Accion
	Motivo string
}

// Decide es el corazón de la política. Usa tres métricas: CPU, latencia y
// hosts saludables, con histéresis para evitar oscilación.
//
//	Subir = (CPU > CPUSubir  O  latencia > LatenciaSubir)  Y  instancias < max
//	Bajar =  CPU < CPUBajar  Y  latencia < LatenciaBajar
//	        Y  hosts_sanos == instancias  Y  instancias > min
//
// Filosofía: fácil subir (proteger al usuario), difícil bajar (conservador).
//
// Orden de evaluación (las guardas de seguridad van primero):
//  1. Métrica no confiable -> mantener.
//  2. Cooldown -> mantener.
//  3. Subir (CPU alta O latencia alta) si hay margen.
//  4. Bajar (todo tranquilo y hosts sanos) si hay más de la mínima.
//  5. En cualquier otro caso -> mantener.
func Decide(s Snapshot) Decision {
	// 1. Datos no confiables: no decidir en falso.
	if !s.MetricaConfiable {
		return Decision{
			Accion: Mantener,
			Motivo: "metrica no confiable (obsoleta o faltante); se mantiene el estado actual",
		}
	}

	// 2. Cooldown activo.
	if s.EnCooldown {
		return Decision{
			Accion: Mantener,
			Motivo: "en cooldown tras una accion previa; se mantiene el estado actual",
		}
	}

	// 3. ¿Subir? CPU alta O latencia alta (proteger al usuario).
	cpuAlta := s.CPUMax > config.CPUSubir
	latenciaAlta := s.LatenciaAvg > config.LatenciaSubir
	if cpuAlta || latenciaAlta {
		if s.InstanciasCorriendo >= config.MaxInstancias {
			return Decision{
				Accion: Mantener,
				Motivo: "carga alta pero ya se alcanzo el maximo de instancias; se mantiene",
			}
		}
		return Decision{
			Accion: Subir,
			Motivo: fmt.Sprintf("subir: CPU=%.1f%% (umbral %.0f) o latencia=%.2fs (umbral %.2f)",
				s.CPUMax, config.CPUSubir, s.LatenciaAvg, config.LatenciaSubir),
		}
	}

	// 4. ¿Bajar? Todo tranquilo Y todos los hosts sanos Y por encima del mínimo.
	cpuBaja := s.CPUMax < config.CPUBajar
	latenciaBaja := s.LatenciaAvg < config.LatenciaBajar
	hostsTodosSanos := s.HostsSaludables == s.InstanciasCorriendo
	if cpuBaja && latenciaBaja {
		if s.InstanciasCorriendo <= config.MinInstancias {
			return Decision{
				Accion: Mantener,
				Motivo: "carga baja pero ya se esta en el minimo de instancias; se mantiene",
			}
		}
		if !hostsTodosSanos {
			return Decision{
				Accion: Mantener,
				Motivo: fmt.Sprintf("carga baja pero no todos los hosts estan sanos (%d sanos de %d); no se baja",
					s.HostsSaludables, s.InstanciasCorriendo),
			}
		}
		return Decision{
			Accion: Bajar,
			Motivo: fmt.Sprintf("bajar: CPU=%.1f%% y latencia=%.2fs bajas, %d hosts sanos == %d instancias",
				s.CPUMax, s.LatenciaAvg, s.HostsSaludables, s.InstanciasCorriendo),
		}
	}

	// 5. Zona intermedia (just-in-need): capacidad adecuada.
	return Decision{
		Accion: Mantener,
		Motivo: "metricas dentro de la banda normal; capacidad adecuada, se mantiene",
	}
}
