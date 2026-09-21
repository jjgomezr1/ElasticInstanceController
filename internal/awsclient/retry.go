package awsclient

import (
	"context"
	"fmt"
	"time"

	"autoscaling-controller/internal/config"
)

// Este archivo implementa el manejo de fallos de AWS que pide el diseño:
// hasta MaxReintentos reintentos, separados EsperaEntreReintentos, antes de
// abortar la operación. Si todos fallan, se devuelve el último error y el
// llamador mantiene el estado actual.
//
// NOTA: por ahora se reintenta ante CUALQUIER error. Afinar qué errores son
// "reintentables" (p.ej. no reintentar AccessDenied, que no se arregla solo)
// es una mejora futura sencilla: bastaría inspeccionar el error aquí.

// ReintentarConValor ejecuta una operación que devuelve (T, error), con
// reintentos. Devuelve el resultado del primer intento exitoso, o el último
// error si se agotan los intentos.
//
// Es genérica (Go generics) para servir a cualquier operación sin importar el
// tipo que devuelva: un ID (string), un Snapshot, etc.
//
// nombre se usa solo para mensajes de error legibles.
func ReintentarConValor[T any](ctx context.Context, nombre string, op func() (T, error)) (T, error) {
	var ultimo error
	var cero T

	// Intentos totales = 1 (inicial) + MaxReintentos.
	totalIntentos := 1 + config.MaxReintentos

	for intento := 1; intento <= totalIntentos; intento++ {
		resultado, err := op()
		if err == nil {
			return resultado, nil
		}
		ultimo = err

		// Si aún quedan intentos, esperar antes del siguiente (respetando el
		// context: si se cancela o expira, abortamos ya).
		if intento < totalIntentos {
			select {
			case <-ctx.Done():
				return cero, fmt.Errorf("%s: context cancelado durante reintentos: %w", nombre, ctx.Err())
			case <-time.After(config.EsperaEntreReintentos):
				// Continuar con el siguiente intento.
			}
		}
	}

	return cero, fmt.Errorf("%s: fallaron los %d intentos: %w", nombre, totalIntentos, ultimo)
}

// Reintentar es la variante para operaciones que solo devuelven error (sin
// valor). Se apoya en ReintentarConValor usando un tipo trivial.
func Reintentar(ctx context.Context, nombre string, op func() error) error {
	_, err := ReintentarConValor(ctx, nombre, func() (struct{}, error) {
		return struct{}{}, op()
	})
	return err
}
