package awsclient

import (
	"context"
	"fmt"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	"autoscaling-controller/internal/config"
	"autoscaling-controller/internal/policy"
)

// ObtenerSnapshot es la Pieza 2: dada la lista de IDs de instancias
// gestionadas (de la Pieza 1), consulta a CloudWatch el uso de CPU de cada una
// durante la ventana de observación y arma el policy.Snapshot que la Pieza 3
// (Decide) necesita.
//
// En esta fase la CPU es la única métrica. Los campos de ALB del Snapshot
// quedan en cero (sin uso todavía). El cooldown también queda en false: quien
// lo determina es el estado en memoria, que aún no existe y solo importará
// cuando el controlador pueda actuar (Piezas 4-6).
func ObtenerSnapshot(ctx context.Context, cliente *cloudwatch.Client, ids []string) (policy.Snapshot, error) {
	snap := policy.Snapshot{
		InstanciasCorriendo: len(ids),
		EnCooldown:          false,
	}

	// Sin instancias corriendo no hay CPU que medir. No llamamos a CloudWatch
	// y devolvemos la métrica como no confiable: no se debe decidir por CPU
	// cuando no hay de dónde leerla.
	if len(ids) == 0 {
		snap.MetricaConfiable = false
		return snap, nil
	}

	// Ventana de tiempo: desde (ahora - ventana) hasta ahora.
	//
	// La ventana (4 min) es CUÁNTO miramos hacia atrás. Dentro de ella pedimos
	// puntos con granularidad fina (1 min = 60s), que es el ritmo con el que
	// publica el monitoreo detallado de EC2. Así obtenemos varios puntos
	// recientes en lugar de un único bucket grande cuyo timestamp quedaría
	// siempre "viejo" (CloudWatch alinea los periodos a fronteras de reloj).
	ahora := time.Now()
	inicio := ahora.Add(-config.VentanaObservacion)

	// Construimos una consulta por instancia. Cada MetricDataQuery pide el
	// promedio de CPUUtilization de una instancia por cada Period.
	var consultas []cwtypes.MetricDataQuery
	for i, id := range ids {
		// El Id de la consulta debe ser único y empezar por letra minúscula;
		// "m0", "m1", ... cumple. Nos sirve luego para casar resultados.
		idConsulta := fmt.Sprintf("m%d", i)
		consultas = append(consultas, cwtypes.MetricDataQuery{
			Id: awssdk.String(idConsulta),
			MetricStat: &cwtypes.MetricStat{
				Metric: &cwtypes.Metric{
					Namespace:  awssdk.String("AWS/EC2"),
					MetricName: awssdk.String("CPUUtilization"),
					Dimensions: []cwtypes.Dimension{
						{
							Name:  awssdk.String("InstanceId"),
							Value: awssdk.String(id),
						},
					},
				},
				Period: awssdk.Int32(config.PeriodoPuntoSegundos),
				Stat:   awssdk.String("Average"),
			},
		})
	}

	entrada := &cloudwatch.GetMetricDataInput{
		StartTime:         awssdk.Time(inicio),
		EndTime:           awssdk.Time(ahora),
		MetricDataQueries: consultas,
		// Puntos ordenados del más reciente al más antiguo, para que
		// Values[0]/Timestamps[0] sea de verdad el dato más nuevo.
		ScanBy: cwtypes.ScanByTimestampDescending,
	}

	salida, err := cliente.GetMetricData(ctx, entrada)
	if err != nil {
		return policy.Snapshot{}, fmt.Errorf("fallo al consultar metricas en CloudWatch: %w", err)
	}

	// Recorremos los resultados: uno por instancia. De cada uno tomamos su
	// valor más reciente (el promedio de esa instancia en la ventana) y su
	// timestamp, para luego calcular el máximo y la antigüedad.
	var cpuMax float64
	var huboAlgunDato bool
	var datoMasReciente time.Time

	for _, resultado := range salida.MetricDataResults {
		// Values[0]/Timestamps[0] es el punto más reciente (ScanBy Descending).
		// Si no hay valores, esa instancia no reportó dentro de la ventana.
		if len(resultado.Values) == 0 {
			continue
		}

		valor := resultado.Values[0]
		ts := resultado.Timestamps[0]

		if valor > cpuMax {
			cpuMax = valor
		}
		huboAlgunDato = true
		if ts.After(datoMasReciente) {
			datoMasReciente = ts
		}
	}

	// Determinamos si la métrica es confiable: debe existir al menos un dato y
	// el más reciente no debe superar la antigüedad máxima permitida.
	confiable := huboAlgunDato &&
		ahora.Sub(datoMasReciente) <= config.MaxAntiguedadMetrica

	snap.CPUMax = cpuMax
	snap.MetricaConfiable = confiable

	return snap, nil
}
