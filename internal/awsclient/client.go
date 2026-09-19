// Package awsclient agrupa TODO el código que habla con AWS. Ninguna otra
// parte del controlador importa el SDK directamente: así, si algo toca AWS,
// se sabe que vive aquí. El paquete policy (la lógica de decisión) permanece
// puro y ajeno a este paquete.
package awsclient

import (
	"context"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

// Clientes agrupa los clientes de los servicios de AWS que usa el controlador.
// Por ahora solo EC2; cuando agreguemos CloudWatch y (más adelante) el ALB,
// se añaden aquí sus clientes y se construyen una sola vez al arrancar.
type Clientes struct {
	EC2 *ec2.Client
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
		EC2: ec2.NewFromConfig(cfg),
	}, nil
}

// _ evita que el import de awssdk quede sin uso mientras solo tenemos EC2.
// (Se usará de verdad cuando pasemos punteros de tipos aws.* en las piezas
// siguientes; se deja el import listo para no reescribir la cabecera.)
var _ = awssdk.String
