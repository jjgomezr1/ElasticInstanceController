package awsclient

import (
	"context"
	"fmt"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"autoscaling-controller/internal/config"
)

// ListManagedInstances es la Pieza 1: responde "¿cuáles instancias son mías y
// están corriendo?". Le pregunta a EC2 por todas las instancias que:
//   - tienen el tag ManagedBy=autoscaling-controller, y
//   - están en estado "running".
//
// Devuelve la lista de IDs de instancia (por ejemplo "i-0abc123..."). Esa
// lista alimenta la Pieza 2 (métricas de CPU) y su longitud es la cantidad de
// instancias corriendo que usa policy.Decide().
func ListManagedInstances(ctx context.Context, cliente *ec2.Client) ([]string, error) {
	// Toda la operación de inventario va envuelta en reintentos: si cualquier
	// página falla por un fallo transitorio de AWS, se reintenta el inventario
	// completo.
	return ReintentarConValor(ctx, "inventario EC2", func() ([]string, error) {
		return listarInstancias(ctx, cliente)
	})
}

// listarInstancias hace la consulta real a EC2 (sin reintentos; de eso se
// encarga ListManagedInstances).
func listarInstancias(ctx context.Context, cliente *ec2.Client) ([]string, error) {
	// Los filtros se aplican del lado de AWS: pedimos directamente lo que nos
	// interesa en vez de traer todo y filtrar en Go.
	entrada := &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			{
				// "tag:<clave>" filtra por el valor de un tag específico.
				Name:   awssdk.String("tag:" + config.TagClave),
				Values: []string{config.TagValor},
			},
			{
				// Solo instancias efectivamente prendidas.
				Name:   awssdk.String("instance-state-name"),
				Values: []string{"running"},
			},
		},
	}

	// DescribeInstances puede devolver los resultados en varias páginas. El
	// paginator recorre todas por nosotros para no perder instancias.
	paginator := ec2.NewDescribeInstancesPaginator(cliente, entrada)

	var ids []string
	for paginator.HasMorePages() {
		pagina, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("fallo al describir instancias en EC2: %w", err)
		}

		// EC2 agrupa las instancias en "Reservations"; cada Reservation
		// contiene una o más Instances. De ahí el doble bucle.
		for _, reserva := range pagina.Reservations {
			for _, instancia := range reserva.Instances {
				if instancia.InstanceId != nil {
					ids = append(ids, *instancia.InstanceId)
				}
			}
		}
	}

	return ids, nil
}
