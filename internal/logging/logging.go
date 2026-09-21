// Package logging registra, de forma estructurada, cada ciclo del controlador.
// Cumple el requisito del reto de que toda decisión quede registrada y sea
// explicable a partir del estado observado: se loguea SIEMPRE, incluso cuando
// la decisión es mantener.
package logging

import (
	"fmt"
	"time"

	"autoscaling-controller/internal/config"
	"autoscaling-controller/internal/policy"
)

// RegistrarCiclo imprime un renglón con todo lo relevante de un ciclo:
//   - hora del ciclo,
//   - métricas y ventana consideradas (CPU máxima, confiabilidad, ventana),
//   - capacidad existente (instancias corriendo),
//   - decisión y su justificación,
//   - acción ejecutada y su resultado.
//
// El parámetro resultado describe qué pasó al actuar: por ejemplo el ID de la
// instancia lanzada/terminada, "sin accion", o el texto de un error.
func RegistrarCiclo(ahora time.Time, snap policy.Snapshot, decision policy.Decision, resultado string) {
	fmt.Printf(
		"[%s] cpu_max=%.2f%% confiable=%t ventana=%s | instancias=%d/%d cooldown=%t | decision=%s (%s) | resultado=%s\n",
		ahora.Format(time.RFC3339),
		snap.CPUMax,
		snap.MetricaConfiable,
		config.VentanaObservacion,
		snap.InstanciasCorriendo,
		config.MaxInstancias,
		snap.EnCooldown,
		decision.Accion,
		decision.Motivo,
		resultado,
	)
}
