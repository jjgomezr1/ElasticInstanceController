// Command scaletest ejercita el pipeline completo de decisión y actuación de
// forma CONTROLADA, sin necesitar carga real. Es la Pieza 7 (inyección de
// métricas sintéticas) combinada con un disparador manual.
//
// Flujo: inventario (Pieza 1) -> CPU (real de la Pieza 2, o inyectada con
// -cpu) -> Decide (Pieza 3) -> actuar (Pieza 4/5) si se pasa -actuar.
//
// Ejemplos:
//
//	# Ver qué decidiría con CPU real, SIN actuar (dry-run):
//	./scaletest
//
//	# Simular CPU al 85% y ver la decisión, SIN actuar:
//	./scaletest -cpu 85
//
//	# Simular CPU al 85% y EJECUTAR de verdad (lanza una instancia real):
//	./scaletest -cpu 85 -actuar
//
// El flag -actuar es un seguro: sin él, scaletest nunca toca la
// infraestructura, solo informa qué haría.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"autoscaling-controller/internal/awsclient"
	"autoscaling-controller/internal/policy"
)

func main() {
	// -cpu: si es >= 0, reemplaza la CPU real por este valor (inyección).
	//       Por defecto -1 = usar la CPU real de CloudWatch.
	cpuInyectada := flag.Float64("cpu", -1, "CPU maxima sintetica (0-100). Si se omite, usa la CPU real.")
	// -actuar: seguro. Sin este flag, es dry-run (solo informa).
	actuar := flag.Bool("actuar", false, "Si se pasa, EJECUTA la accion real (lanzar/terminar). Sin el, solo informa.")
	flag.Parse()

	ctx, cancelar := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelar()

	fmt.Println("== scaletest: prueba controlada del pipeline de escalado ==")

	clientes, err := awsclient.NuevosClientes(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR cargando AWS: %v\n", err)
		os.Exit(1)
	}

	// --- Pieza 1: inventario ---
	ids, err := awsclient.ListManagedInstances(ctx, clientes.EC2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR inventario: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Instancias gestionadas corriendo: %d\n", len(ids))
	for _, id := range ids {
		fmt.Printf("  - %s\n", id)
	}

	// --- Pieza 2: CPU (real o inyectada) ---
	snap, err := awsclient.ObtenerSnapshot(ctx, clientes.CloudWatch, ids)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR metricas: %v\n", err)
		os.Exit(1)
	}

	// Pieza 7: si se inyectó una CPU, sobreescribimos la real. La marcamos
	// como confiable a mano, porque el dato viene de quien prueba, no de una
	// métrica que pueda estar obsoleta.
	if *cpuInyectada >= 0 {
		fmt.Printf("\n[INYECCION] Sobrescribiendo CPU real (%.2f%%) por CPU sintetica %.2f%%\n",
			snap.CPUMax, *cpuInyectada)
		snap.CPUMax = *cpuInyectada
		snap.MetricaConfiable = true
	}

	fmt.Println("\nSnapshot para decidir:")
	fmt.Printf("  Instancias corriendo : %d\n", snap.InstanciasCorriendo)
	fmt.Printf("  CPU maxima           : %.2f%%\n", snap.CPUMax)
	fmt.Printf("  Metrica confiable    : %t\n", snap.MetricaConfiable)
	fmt.Printf("  En cooldown          : %t\n", snap.EnCooldown)

	// --- Pieza 3: decidir ---
	decision := policy.Decide(snap)
	fmt.Printf("\nDecision: %s\n", decision.Accion)
	fmt.Printf("Motivo  : %s\n", decision.Motivo)

	// --- Pieza 4/5: actuar (solo si se pidió con -actuar) ---
	if !*actuar {
		fmt.Println("\n(dry-run: no se ejecuta ninguna accion. Pasa -actuar para ejecutar de verdad.)")
		return
	}

	switch decision.Accion {
	case policy.Subir:
		fmt.Println("\n[ACTUAR] Lanzando una instancia nueva...")
		id, err := awsclient.LanzarInstancia(ctx, clientes.EC2, clientes.SSM)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR al lanzar: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Instancia lanzada: %s\n", id)
		fmt.Println("(Tarda 1-2 min en aparecer como 'running' y en el inventario.)")

	case policy.Bajar:
		fmt.Println("\n[ACTUAR] Terminando una instancia (la mas nueva)...")
		id, err := awsclient.TerminarInstancia(ctx, clientes.EC2, ids)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR al terminar: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Instancia terminada: %s\n", id)
		fmt.Println("(Tarda ~1 min en pasar a 'terminated' y salir del inventario.)")

	default:
		fmt.Println("\n[ACTUAR] La decision es mantener; no hay nada que ejecutar.")
	}
}
