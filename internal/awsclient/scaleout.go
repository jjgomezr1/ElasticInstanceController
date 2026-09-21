package awsclient

import (
	"context"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"autoscaling-controller/internal/config"
)

// LanzarInstancia es la Pieza 4 (fase ALB): crea UNA instancia EC2 nueva a
// partir de la AMI dorada (que ya trae la app), la lanza en la subred y el
// security group del proyecto, la etiqueta con ManagedBy y la REGISTRA en el
// Target Group del ALB.
//
// El registro es inmediato: el ALB no le enviará tráfico hasta que su health
// check la marque "healthy", así que registrar de una es seguro y simple (el
// ALB gestiona la "capacidad efectiva").
//
// Devuelve el ID de la instancia lanzada.
func LanzarInstancia(ctx context.Context, cEC2 *ec2.Client, cELB *elbv2.Client) (string, error) {
	// 1. Lanzar la instancia desde la AMI dorada.
	entrada := &ec2.RunInstancesInput{
		ImageId:      awssdk.String(config.AMIImagen),
		InstanceType: ec2types.InstanceType(config.TipoInstancia),
		KeyName:      awssdk.String(config.KeyPair),
		// Siempre exactamente una instancia por decisión (regla del reto).
		MinCount: awssdk.Int32(1),
		MaxCount: awssdk.Int32(1),
		// Subred de la VPC del proyecto + SG de instancias (solo acepta HTTP
		// desde el ALB).
		SubnetId:         awssdk.String(config.SubnetID),
		SecurityGroupIds: []string{config.SecurityGroupID},
		// Monitoreo detallado: CPU cada 1 min, necesario para la ventana de 4
		// min de la Pieza 2.
		Monitoring: &ec2types.RunInstancesMonitoringEnabled{
			Enabled: awssdk.Bool(true),
		},
		// El tag que hace que la Pieza 1 la cuente como "mía".
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeInstance,
				Tags: []ec2types.Tag{
					{Key: awssdk.String(config.TagClave), Value: awssdk.String(config.TagValor)},
					{Key: awssdk.String("Name"), Value: awssdk.String("sujeto-autoscaling")},
				},
			},
		},
	}

	salida, err := ReintentarConValor(ctx, "RunInstances", func() (*ec2.RunInstancesOutput, error) {
		return cEC2.RunInstances(ctx, entrada)
	})
	if err != nil {
		return "", fmt.Errorf("fallo al lanzar la instancia (RunInstances): %w", err)
	}
	if len(salida.Instances) == 0 || salida.Instances[0].InstanceId == nil {
		return "", fmt.Errorf("RunInstances no devolvio ninguna instancia")
	}
	id := *salida.Instances[0].InstanceId

	// 2. Registrar la instancia en el Target Group. El ALB empezará a hacerle
	// health checks; solo le enviará tráfico cuando esté "healthy".
	err = Reintentar(ctx, "RegisterTargets", func() error {
		_, e := cELB.RegisterTargets(ctx, &elbv2.RegisterTargetsInput{
			TargetGroupArn: awssdk.String(config.TargetGroupARN),
			Targets: []elbtypes.TargetDescription{
				{Id: awssdk.String(id)},
			},
		})
		return e
	})
	if err != nil {
		// La instancia ya se lanzó; informamos el fallo de registro para que
		// el ciclo lo loguee. La instancia queda gestionada (tiene el tag) y
		// podrá registrarse en un ciclo posterior o depurarse a mano.
		return id, fmt.Errorf("instancia %s lanzada pero fallo al registrarla en el target group: %w", id, err)
	}

	return id, nil
}
