// Package state guarda el estado que el controlador retiene EN MEMORIA entre
// ciclos: los timestamps de la última acción de escalado, para calcular los
// cooldowns. Si el proceso se reinicia, este estado se pierde y el controlador
// vuelve a un estado de observación limpio (sin cooldown activo), tal como
// define el diseño.
package state

import (
	"time"

	"autoscaling-controller/internal/config"
)

// Estado retiene los timestamps de la última subida y la última bajada.
// Un tiempo "cero" (time.Time{}) significa que esa acción nunca ocurrió en la
// vida de este proceso.
type Estado struct {
	ultimaSubida time.Time
	ultimaBajada time.Time
}

// Nuevo crea un Estado limpio, sin cooldowns activos.
func Nuevo() *Estado {
	return &Estado{}
}

// RegistrarSubida marca que se acaba de lanzar una instancia. A partir de
// ahora corre el cooldown de subida.
//
// NOTA (nivel simple): el cooldown se cuenta desde el momento de la acción.
// El diseño pide contarlo desde que la instancia nueva tiene "capacidad
// efectiva" (status check 2/2); eso es un refinamiento posterior.
func (e *Estado) RegistrarSubida(ahora time.Time) {
	e.ultimaSubida = ahora
}

// RegistrarBajada marca que se acaba de terminar una instancia. A partir de
// ahora corre el cooldown de bajada.
func (e *Estado) RegistrarBajada(ahora time.Time) {
	e.ultimaBajada = ahora
}

// EnCooldown indica si, en el instante "ahora", todavía estamos dentro del
// periodo de enfriamiento de alguna de las dos acciones. Mientras sea true,
// policy.Decide() mantendrá el estado actual sin subir ni bajar.
func (e *Estado) EnCooldown(ahora time.Time) bool {
	if !e.ultimaSubida.IsZero() && ahora.Sub(e.ultimaSubida) < config.CooldownSubida {
		return true
	}
	if !e.ultimaBajada.IsZero() && ahora.Sub(e.ultimaBajada) < config.CooldownBajada {
		return true
	}
	return false
}
