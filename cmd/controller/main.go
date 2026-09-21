// Command controller es el punto de entrada del controlador real (Pieza 6).
// Corre indefinidamente: cada IntervaloEvaluacion ejecuta un ciclo completo de
// observación y decisión, y actúa sobre la infraestructura si corresponde.
//
// Uso:
//
//	./controller
//
// Se detiene ordenadamente con Ctrl+C (SIGINT) o SIGTERM.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"autoscaling-controller/internal/awsclient"
	"autoscaling-controller/internal/config"
	"autoscaling-controller/internal/logging"
	"autoscaling-controller/internal/policy"
	"autoscaling-controller/internal/state"
)

func main() {
	fmt.Println("== Auto-Scaling Controller ==")
	fmt.Printf("Intervalo=%s ventana=%s umbral_subir=%.0f%% umbral_bajar=%.0f%% min=%d max=%d\n",
		config.IntervaloEvaluacion, config.VentanaObservacion,
		config.CPUSubir, config.CPUBajar, config.MinInstancias, config.MaxInstancias)

	// Clientes de AWS: se construyen una sola vez y se reutilizan.
	ctxInicio, cancelarInicio := context.WithTimeout(context.Background(), 30*time.Second)
	clientes, err := awsclient.NuevosClientes(ctxInicio)
	cancelarInicio()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR fatal al inicializar AWS: %v\n", err)
		os.Exit(1)
	}

	// Estado en memoria (cooldowns). Persiste entre ciclos, se pierde al
	// reiniciar el proceso (vuelta a observación limpia).
	est := state.Nuevo()

	// Canal para detener el bucle ordenadamente ante Ctrl+C / SIGTERM.
	parar := make(chan os.Signal, 1)
	signal.Notify(parar, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("Controlador en marcha. Ctrl+C para detener.")

	for {
		inicioCiclo := time.Now()
		ejecutarCiclo(clientes, est, inicioCiclo)

		// Descontar del intervalo el tiempo ya gastado en el ciclo, para no
		// acumular atraso progresivo (como pide el diseño).
		gastado := time.Since(inicioCiclo)
		espera := config.IntervaloEvaluacion - gastado
		if espera < 0 {
			espera = 0
		}

		// Esperar hasta el próximo ciclo, o salir si llega una señal de parada.
		select {
		case <-parar:
			fmt.Println("\nSeñal recibida. Deteniendo el controlador.")
			return
		case <-time.After(espera):
			// Continuar con el siguiente ciclo.
		}
	}
}

// ejecutarCiclo corre un ciclo completo: inventario -> métricas -> cooldown ->
// decidir -> actuar -> loguear. Nunca hace panic: cualquier fallo se registra
// y el bucle continúa observando.
func ejecutarCiclo(clientes *awsclient.Clientes, est *state.Estado, ahora time.Time) {
	// Cada ciclo tiene su propio context acotado, para que una llamada colgada
	// de AWS no congele el bucle. El timeout debe dar espacio a los reintentos
	// (hasta 2 × EsperaEntreReintentos = 40s de esperas, más las llamadas).
	ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelar()

	// 1. Inventario (Pieza 1).
	ids, err := awsclient.ListManagedInstances(ctx, clientes.EC2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] ciclo abortado: fallo inventario: %v\n",
			ahora.Format(time.RFC3339), err)
		return
	}

	// 2. Métricas (Pieza 2): CPU + latencia + hosts saludables -> Snapshot.
	snap, err := awsclient.ObtenerSnapshot(ctx, clientes.CloudWatch, clientes.ELB, ids)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[%s] ciclo abortado: fallo metricas: %v\n",
			ahora.Format(time.RFC3339), err)
		return
	}

	// 3. Marcar cooldown según el estado en memoria.
	snap.EnCooldown = est.EnCooldown(ahora)

	// 4. Decidir (Pieza 3).
	decision := policy.Decide(snap)

	// 5. Actuar (Pieza 4/5) si corresponde. Registra el resultado para el log.
	resultado := "sin accion"
	switch decision.Accion {
	case policy.Subir:
		id, err := awsclient.LanzarInstancia(ctx, clientes.EC2, clientes.ELB)
		if err != nil {
			resultado = "ERROR al lanzar: " + err.Error()
		} else {
			resultado = "instancia lanzada y registrada " + id
			est.RegistrarSubida(ahora)
		}

	case policy.Bajar:
		id, err := awsclient.TerminarInstancia(ctx, clientes.EC2, clientes.ELB, ids)
		if err != nil {
			resultado = "ERROR al terminar: " + err.Error()
		} else {
			resultado = "instancia desregistrada y terminada " + id
			est.RegistrarBajada(ahora)
		}
	}

	// 6. Loguear el ciclo completo (siempre, decida lo que decida).
	logging.RegistrarCiclo(ahora, snap, decision, resultado)
}
