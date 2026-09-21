package awsclient

import (
	"context"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"autoscaling-controller/internal/config"
)

// resolverAMI le pregunta a SSM el ID de AMI más reciente de Ubuntu 24.04
// (amd64) en la región actual, usando el parámetro público definido en config.
// Así no hardcodeamos un ami-... que cambia por región y con el tiempo.
func resolverAMI(ctx context.Context, cliente *ssm.Client) (string, error) {
	salida, err := cliente.GetParameter(ctx, &ssm.GetParameterInput{
		Name: awssdk.String(config.ParametroSSMImagen),
	})
	if err != nil {
		return "", fmt.Errorf("no se pudo resolver la AMI desde SSM (%s): %w",
			config.ParametroSSMImagen, err)
	}
	if salida.Parameter == nil || salida.Parameter.Value == nil {
		return "", fmt.Errorf("SSM devolvio un parametro vacio para la AMI")
	}
	return *salida.Parameter.Value, nil
}

// LanzarInstancia es la Pieza 4: crea UNA instancia EC2 nueva, gestionada por
// el controlador. La marca con el tag ManagedBy para que la Pieza 1 la
// reconozca, y le activa el monitoreo detallado para que la Pieza 2 tenga
// datos de CPU cada minuto desde el arranque.
//
// Devuelve el ID de la instancia lanzada. NO espera a que quede "healthy" /
// con capacidad efectiva: eso se maneja aparte (cooldown/estado) más adelante.
func LanzarInstancia(ctx context.Context, cEC2 *ec2.Client, cSSM *ssm.Client) (string, error) {
	// 1. Resolver qué imagen usar.
	amiID, err := resolverAMI(ctx, cSSM)
	if err != nil {
		return "", err
	}

	// 2. Pedir la creación de la instancia.
	entrada := &ec2.RunInstancesInput{
		ImageId:      awssdk.String(amiID),
		InstanceType: ec2types.InstanceType(config.TipoInstancia),
		KeyName:      awssdk.String(config.KeyPair),
		// Siempre exactamente una instancia por decisión (regla del reto).
		MinCount:         awssdk.Int32(1),
		MaxCount:         awssdk.Int32(1),
		SecurityGroupIds: []string{config.SecurityGroupID},
		// Monitoreo detallado: métricas de CPU cada 1 min (necesario para que
		// la ventana de 4 min de la Pieza 2 tenga datos frescos).
		Monitoring: &ec2types.RunInstancesMonitoringEnabled{
			Enabled: awssdk.Bool(true),
		},
		// El tag que hace que la Pieza 1 la cuente como "mía".
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeInstance,
				Tags: []ec2types.Tag{
					{
						Key:   awssdk.String(config.TagClave),
						Value: awssdk.String(config.TagValor),
					},
					{
						Key:   awssdk.String("Name"),
						Value: awssdk.String("sujeto-autoscaling"),
					},
				},
			},
		},
	}

	salida, err := cEC2.RunInstances(ctx, entrada)
	if err != nil {
		return "", fmt.Errorf("fallo al lanzar la instancia (RunInstances): %w", err)
	}
	if len(salida.Instances) == 0 || salida.Instances[0].InstanceId == nil {
		return "", fmt.Errorf("RunInstances no devolvio ninguna instancia")
	}

	return *salida.Instances[0].InstanceId, nil
}
