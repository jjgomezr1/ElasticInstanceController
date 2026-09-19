// Package config centraliza todos los umbrales, cooldowns y constantes del
// controlador de auto-escalado. Es la única fuente de verdad: cualquier otra
// pieza que necesite un valor de política lo importa desde aquí, de modo que
// un cambio de umbral se haga en un solo lugar.
package config

import "time"

// --- Límites de capacidad (impuestos por el reto) ---

const (
	// MinInstancias es la cantidad mínima de instancias que el controlador
	// debe mantener corriendo en todo momento.
	MinInstancias = 1
	// MaxInstancias es el tope superior de instancias permitido por el reto.
	MaxInstancias = 5
)

// --- Umbrales de CPU (en porcentaje, 0-100) ---
//
// En esta fase (sin ALB) la CPU es la ÚNICA métrica de decisión. El margen
// del 70% para subir deja un 30% de holgura para que una instancia nueva
// alcance a levantarse antes de que la carga llegue a un estado crítico.

const (
	// CPUSubir: si la CPU máxima observada supera este valor, se considera
	// subir una instancia.
	CPUSubir = 70.0
	// CPUBajar: si la CPU máxima observada queda por debajo de este valor, se
	// considera bajar una instancia.
	CPUBajar = 30.0
)

// --- Ventana de observación y consulta de métricas ---

const (
	// VentanaObservacion es el periodo hacia atrás que se mira para decidir.
	// Punto medio entre reaccionar a picos aislados y tardar demasiado.
	VentanaObservacion = 4 * time.Minute

	// PeriodoMetrica es el "Period" que se le pide a CloudWatch (en segundos).
	// Coincide con la ventana para obtener un promedio por instancia sobre
	// toda la ventana de observación.
	PeriodoMetricaSegundos = 240

	// MaxAntiguedadMetrica: si el dato más reciente de una métrica es más
	// viejo que esto, se considera no confiable y el controlador mantiene el
	// estado actual sin decidir en falso.
	MaxAntiguedadMetrica = 2 * time.Minute
)

// --- Cooldowns (anti-oscilación) ---
//
// El cooldown de bajada es mayor que el de subida porque el
// under-provisioning afecta al usuario de inmediato, mientras que el
// over-provisioning no.

const (
	// CooldownSubida se cuenta desde que la instancia nueva queda con
	// "capacidad efectiva" (pasa el status check), no desde que se lanzó.
	CooldownSubida = 3 * time.Minute
	// CooldownBajada se cuenta desde que la instancia terminada se confirma
	// terminada.
	CooldownBajada = 6 * time.Minute
)

// --- Ritmo del ciclo principal y reintentos ante fallos de AWS ---

const (
	// IntervaloEvaluacion es cada cuánto corre el ciclo completo del
	// controlador (inventario -> métricas -> decidir -> actuar -> loguear).
	IntervaloEvaluacion = 1 * time.Minute

	// MaxReintentos es la cantidad de reintentos ante un fallo de AWS antes
	// de abortar la acción y mantener el estado actual.
	MaxReintentos = 2
	// EsperaEntreReintentos es la pausa entre reintentos.
	EsperaEntreReintentos = 20 * time.Second
)

// --- Etiquetado de instancias gestionadas ---
//
// Al no usar un Auto Scaling Group, el "agrupamiento" de nuestras instancias
// se hace con un tag. La Pieza 1 (inventario) busca exactamente este tag.

const (
	// TagClave es la clave del tag que marca una instancia como gestionada
	// por este controlador.
	TagClave = "ManagedBy"
	// TagValor es el valor esperado del tag.
	TagValor = "autoscaling-controller"
)
