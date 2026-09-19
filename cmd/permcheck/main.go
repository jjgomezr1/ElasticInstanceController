// Command permcheck es una herramienta APARTE y desechable para probar, de
// forma aislada, que las llamadas a AWS del controlador funcionan con las
// credenciales y permisos actuales. NO es parte del controlador en producción.
//
// Uso (con la sesión del Learner Lab activa):
//
//	go run ./cmd/permcheck
//
// Por ahora prueba la Pieza 1: lista las instancias gestionadas y corriendo.
// Imprime los IDs encontrados o el error de AWS de forma legible.
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

	fmt.Println("== permcheck: prueba de la Pieza 1 (inventario) ==")

	// 1. Construir los clientes de AWS (carga credenciales + region us-east-1).
	clientes, err := awsclient.NuevosClientes(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR cargando configuracion/credenciales de AWS: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Clientes de AWS creados correctamente (credenciales y region OK).")

	// 2. Llamar a la Pieza 1.
	fmt.Printf("Buscando instancias con tag %s=%s en estado running...\n",
		config.TagClave, config.TagValor)
	ids, err := awsclient.ListManagedInstances(ctx, clientes.EC2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR consultando EC2: %v\n", err)
		os.Exit(1)
	}

	// 3. Reportar el resultado.
	if len(ids) == 0 {
		fmt.Println("Resultado: 0 instancias gestionadas corriendo.")
		fmt.Println("(La llamada a AWS funciono; simplemente no hay instancias con ese tag todavia.)")
		return
	}
	fmt.Printf("Resultado: %d instancia(s) gestionada(s) corriendo:\n", len(ids))
	for _, id := range ids {
		fmt.Printf("  - %s\n", id)
	}
}
