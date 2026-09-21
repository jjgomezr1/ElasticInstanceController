// Command permcheck es una herramienta APARTE y desechable para probar, de
// forma aislada, que las llamadas a AWS del controlador funcionan con las
// credenciales y permisos actuales. NO es parte del controlador en producción.
//
// Uso (con la sesión del Learner Lab activa):
//
//	go run ./cmd/permcheck
//
// Prueba dos piezas:
//   - Pieza 1: lista las instancias gestionadas y corriendo (tag + running).
//   - Pieza 2: consulta la CPU en CloudWatch y arma el policy.Snapshot.
//
// Imprime lo encontrado o el error de AWS de forma legible. No actúa sobre la
// infraestructura (no lanza ni termina nada).
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"autoscaling-controller/internal/awsclient"
	"autoscaling-controller/internal/config"
)

func main() {
	// Un context con timeout: si AWS no responde en 30s, se aborta en vez de
	// quedarse colgado. Es la forma idiomática de acotar operaciones de red.
	ctx, cancelar := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelar()

	fmt.Println("== permcheck: prueba de las Piezas 1 y 2 ==")

	// 1. Construir los clientes de AWS (carga credenciales + region us-east-1).
	clientes, err := awsclient.NuevosClientes(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR cargando configuracion/credenciales de AWS: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Clientes de AWS creados correctamente (credenciales y region OK).")

	// --- Pieza 1: inventario ---
	fmt.Printf("\n[Pieza 1] Buscando instancias con tag %s=%s en estado running...\n",
		config.TagClave, config.TagValor)
	ids, err := awsclient.ListManagedInstances(ctx, clientes.EC2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR consultando EC2: %v\n", err)
		os.Exit(1)
	}

	if len(ids) == 0 {
		fmt.Println("Resultado: 0 instancias gestionadas corriendo.")
	} else {
		fmt.Printf("Resultado: %d instancia(s) gestionada(s) corriendo:\n", len(ids))
		for _, id := range ids {
			fmt.Printf("  - %s\n", id)
		}
	}

	// --- Pieza 2: métricas de CPU desde CloudWatch ---
	fmt.Printf("\n[Pieza 2] Consultando CPU en CloudWatch (ventana %s, punto %ds)...\n",
		config.VentanaObservacion, config.PeriodoPuntoSegundos)
	snap, err := awsclient.ObtenerSnapshot(ctx, clientes.CloudWatch, ids)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR consultando CloudWatch: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Snapshot construido:")
	fmt.Printf("  Instancias corriendo : %d\n", snap.InstanciasCorriendo)
	fmt.Printf("  CPU maxima           : %.2f%%\n", snap.CPUMax)
	fmt.Printf("  Metrica confiable    : %t\n", snap.MetricaConfiable)
	fmt.Printf("  En cooldown          : %t\n", snap.EnCooldown)

	if !snap.MetricaConfiable {
		fmt.Println("  (Nota: metrica no confiable. Posibles causas: monitoreo detallado")
		fmt.Println("   apagado -> aun no hay dato dentro de la ventana de 4 min; o la")
		fmt.Println("   instancia acaba de arrancar y no ha publicado CPU todavia.)")
	}
}
