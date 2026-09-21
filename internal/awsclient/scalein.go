package awsclient

import (
	"context"
	"fmt"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"autoscaling-controller/internal/config"
)

// elegirInstanciaATerminar decide cuál instancia quitar entre las candidatas.
// Criterio: la MÁS NUEVA (mayor LaunchTime). Al reducir carga tiene sentido
// deshacer el último scale-out, conservando las instancias veteranas que ya
// están "calientes" y con tráfico establecido.
func elegirInstanciaATerminar(ctx context.Context, cEC2 *ec2.Client, ids []string) (string, error) {
	salida, err := ReintentarConValor(ctx, "DescribeInstances (LaunchTime)", func() (*ec2.DescribeInstancesOutput, error) {
		return cEC2.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: ids,
		})
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

// esperarDraining sondea el estado del target hasta que deje de estar
// "draining" (o hasta agotar EsperaMaxDraining). Durante el draining el ALB no
// envía tráfico nuevo pero deja terminar las peticiones en curso.
func esperarDraining(ctx context.Context, cELB *elbv2.Client, id string) {
	limite := time.Now().Add(config.EsperaMaxDraining)
	for time.Now().Before(limite) {
		salida, err := cELB.DescribeTargetHealth(ctx, &elbv2.DescribeTargetHealthInput{
			TargetGroupArn: awssdk.String(config.TargetGroupARN),
			Targets:        []elbtypes.TargetDescription{{Id: awssdk.String(id)}},
		})
		// Ante un error transitorio consultando, seguimos esperando hasta el
		// tope; no abortamos el draining por eso.
		if err == nil {
			if len(salida.TargetHealthDescriptions) == 0 {
				// El target ya no está en el grupo: draining completo.
				return
			}
			estado := salida.TargetHealthDescriptions[0].TargetHealth.State
			if estado != elbtypes.TargetHealthStateEnumDraining {
				// Ya no está draining (unused/desregistrado): listo.
				return
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(config.IntervaloSondeoDraining):
		}
	}
}

// TerminarInstancia es la Pieza 5 (fase ALB): baja segura. Elige una instancia
// (la más nueva), la DESREGISTRA del Target Group, espera el draining y luego
// la TERMINA. Draining SÍNCRONO (bloquea el ciclo ~30-45s); es aceptable
// porque la app responde en milisegundos y bajar solo ocurre en baja demanda.
//
// Respeta el mínimo de instancias como guarda defensiva.
func TerminarInstancia(ctx context.Context, cEC2 *ec2.Client, cELB *elbv2.Client, ids []string) (string, error) {
	// Guarda defensiva: nunca bajar del mínimo.
	if len(ids) <= config.MinInstancias {
		return "", fmt.Errorf("no se termina: hay %d instancia(s) y el minimo es %d",
			len(ids), config.MinInstancias)
	}

	elegido, err := elegirInstanciaATerminar(ctx, cEC2, ids)
	if err != nil {
		return "", err
	}

	// 1. Desregistrar del Target Group: deja de recibir tráfico nuevo y entra
	// en "draining".
	err = Reintentar(ctx, "DeregisterTargets", func() error {
		_, e := cELB.DeregisterTargets(ctx, &elbv2.DeregisterTargetsInput{
			TargetGroupArn: awssdk.String(config.TargetGroupARN),
			Targets:        []elbtypes.TargetDescription{{Id: awssdk.String(elegido)}},
		})
		return e
	})
	if err != nil {
		return "", fmt.Errorf("fallo al desregistrar la instancia %s del target group: %w", elegido, err)
	}

	// 2. Esperar el draining (síncrono).
	esperarDraining(ctx, cELB, elegido)

	// 3. Terminar la instancia.
	err = Reintentar(ctx, "TerminateInstances", func() error {
		_, e := cEC2.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
			InstanceIds: []string{elegido},
		})
		return e
	})
	if err != nil {
		return "", fmt.Errorf("fallo al terminar la instancia %s: %w", elegido, err)
	}

	return elegido, nil
}
