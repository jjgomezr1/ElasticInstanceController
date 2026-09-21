// Package awsclient agrupa TODO el código que habla con AWS. Ninguna otra
// parte del controlador importa el SDK directamente: así, si algo toca AWS,
// se sabe que vive aquí. El paquete policy (la lógica de decisión) permanece
// puro y ajeno a este paquete.
package awsclient

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
)

// Clientes agrupa los clientes de los servicios de AWS que usa el controlador.
// Se construyen una sola vez al arrancar y se reutilizan en cada ciclo.
type Clientes struct {
	EC2        *ec2.Client
	CloudWatch *cloudwatch.Client
	// ELB es el cliente de Elastic Load Balancing v2: registra/desregistra
	// instancias en el Target Group y consulta la salud de los targets.
	ELB *elbv2.Client
}

// NuevosClientes carga la configuración por defecto del SDK y construye los
// clientes de servicio.
//
// LoadDefaultConfig resuelve las credenciales automáticamente según el
// entorno, sin que tengamos que pasar llaves a mano:
//   - En la instancia controladora: las toma del IAM Instance Profile
//     (LabInstanceProfile) vía el servicio de metadata de la EC2.
//   - En tu Windows local: las toma de las credenciales temporales que ya
//     tiene configuradas el AWS CLI.
//
// La región se fija explícitamente a us-east-1 (la del Learner Lab).
func NuevosClientes(ctx context.Context) (*Clientes, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
	)
	if err != nil {
		// %w envuelve el error original para no perder su causa; quien reciba
		// este error puede seguir la cadena hasta la falla real del SDK.
		return nil, fmt.Errorf("no se pudo cargar la configuracion de AWS: %w", err)
	}

	return &Clientes{
		EC2:        ec2.NewFromConfig(cfg),
		CloudWatch: cloudwatch.NewFromConfig(cfg),
		ELB:        elbv2.NewFromConfig(cfg),
	}, nil
}
