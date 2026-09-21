package awsclient

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"autoscaling-controller/internal/config"
)

// elegirInstanciaATerminar decide cuál instancia quitar entre las candidatas.
// Criterio: la MÁS NUEVA (mayor LaunchTime). Al reducir carga tiene sentido
// deshacer el último scale-out, conservando las instancias veteranas que ya
// están "calientes" y con tráfico establecido.
//
// Consulta a EC2 el LaunchTime de cada instancia; no ampliamos el inventario
// (Pieza 1) para no tocar código ya validado.
func elegirInstanciaATerminar(ctx context.Context, cEC2 *ec2.Client, ids []string) (string, error) {
	salida, err := cEC2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: ids,
	})
	if err != nil {
		return "", fmt.Errorf("fallo al consultar LaunchTime de las instancias: %w", err)
	}

	var elegido string
	var lanzamientoElegido time.Time

	for _, reserva := range salida.Reservations {
		for _, inst := range reserva.Instances {
			if inst.InstanceId == nil || inst.LaunchTime == nil {
				continue
			}
			// Nos quedamos con la de LaunchTime más reciente.
			if elegido == "" || inst.LaunchTime.After(lanzamientoElegido) {
				elegido = *inst.InstanceId
				lanzamientoElegido = *inst.LaunchTime
			}
		}
	}

	if elegido == "" {
		return "", fmt.Errorf("no se pudo determinar una instancia a terminar")
	}
	return elegido, nil
}

// TerminarInstancia es la Pieza 5: elige una instancia (la más nueva) y la
// termina. Respeta el mínimo de instancias como guarda defensiva, aunque
// policy.Decide() ya bloquea bajar cuando solo queda una.
//
// NOTA: cuando exista el ALB, antes del terminate habrá que desregistrar el
// target y esperar el "draining". En esta fase (sin ALB) el terminate es
// directo. Devuelve el ID de la instancia terminada.
func TerminarInstancia(ctx context.Context, cEC2 *ec2.Client, ids []string) (string, error) {
	// Guarda defensiva: nunca bajar del mínimo.
	if len(ids) <= config.MinInstancias {
		return "", fmt.Errorf("no se termina: hay %d instancia(s) y el minimo es %d",
			len(ids), config.MinInstancias)
	}

	elegido, err := elegirInstanciaATerminar(ctx, cEC2, ids)
	if err != nil {
		return "", err
	}

	_, err = cEC2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{elegido},
	})
	if err != nil {
		return "", fmt.Errorf("fallo al terminar la instancia %s: %w", elegido, err)
	}

	return elegido, nil
}
