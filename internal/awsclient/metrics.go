package awsclient

import (
	"context"
	"fmt"
	"strings"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"

	"autoscaling-controller/internal/config"
	"autoscaling-controller/internal/policy"
)

// dimensionTargetGroup extrae, del ARN del Target Group, el valor que
// CloudWatch usa como dimensión "TargetGroup".
//
// ARN:   arn:aws:elasticloadbalancing:region:cuenta:targetgroup/nombre/hash
// Dim.:  targetgroup/nombre/hash   (todo lo que va desde "targetgroup/")
func dimensionTargetGroup(arnTG string) string {
	i := strings.Index(arnTG, "targetgroup/")
	if i < 0 {
		return ""
	}
	return arnTG[i:]
}

// dimensionLoadBalancer resuelve, vía DescribeLoadBalancers, el valor que
// CloudWatch usa como dimensión "LoadBalancer" para el ALB.
//
// ARN del ALB: arn:...:loadbalancer/app/nombre/hash
// Dimensión:   app/nombre/hash   (desde "app/")
func dimensionLoadBalancer(ctx context.Context, cELB *elbv2.Client) (string, error) {
	salida, err := ReintentarConValor(ctx, "DescribeLoadBalancers", func() (*elbv2.DescribeLoadBalancersOutput, error) {
		return cELB.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{
			Names: []string{config.ALBNombre},
		})
	})
	if err != nil {
		return "", fmt.Errorf("no se pudo describir el ALB %q: %w", config.ALBNombre, err)
	}
	if len(salida.LoadBalancers) == 0 || salida.LoadBalancers[0].LoadBalancerArn == nil {
		return "", fmt.Errorf("no se encontro el ALB %q", config.ALBNombre)
	}
	arn := *salida.LoadBalancers[0].LoadBalancerArn
	i := strings.Index(arn, "app/")
	if i < 0 {
		return "", fmt.Errorf("ARN de ALB con formato inesperado: %s", arn)
	}
	return arn[i:], nil
}

// ObtenerSnapshot es la Pieza 2 (fase ALB): arma el policy.Snapshot con las
// tres métricas de la política final:
//   - CPUMax: máximo entre instancias del promedio de CPU (AWS/EC2).
//   - LatenciaAvg: TargetResponseTime del ALB (AWS/ApplicationELB).
//   - HostsSaludables: HealthyHostCount del Target Group.
//
// La confiabilidad se basa en la CPU (métrica por instancia, la más crítica);
// latencia y hosts se toman como referencia de apoyo.
func ObtenerSnapshot(ctx context.Context, cCW *cloudwatch.Client, cELB *elbv2.Client, ids []string) (policy.Snapshot, error) {
	snap := policy.Snapshot{
		InstanciasCorriendo: len(ids),
		EnCooldown:          false,
	}

	if len(ids) == 0 {
		snap.MetricaConfiable = false
		return snap, nil
	}

	ahora := time.Now()
	inicio := ahora.Add(-config.VentanaObservacion)

	// Resolver la dimensión del ALB (para TargetResponseTime).
	dimLB, err := dimensionLoadBalancer(ctx, cELB)
	if err != nil {
		return policy.Snapshot{}, err
	}
	dimTG := dimensionTargetGroup(config.TargetGroupARN)

	// --- Consultas a CloudWatch (una sola llamada GetMetricData) ---
	var consultas []cwtypes.MetricDataQuery

	// CPU por instancia: m0, m1, ...
	for i, id := range ids {
		consultas = append(consultas, cwtypes.MetricDataQuery{
			Id: awssdk.String(fmt.Sprintf("m%d", i)),
			MetricStat: &cwtypes.MetricStat{
				Metric: &cwtypes.Metric{
					Namespace:  awssdk.String("AWS/EC2"),
					MetricName: awssdk.String("CPUUtilization"),
					Dimensions: []cwtypes.Dimension{
						{Name: awssdk.String("InstanceId"), Value: awssdk.String(id)},
					},
				},
				Period: awssdk.Int32(config.PeriodoPuntoSegundos),
				Stat:   awssdk.String("Average"),
			},
		})
	}

	// Latencia (TargetResponseTime) del ALB: id "lat".
	consultas = append(consultas, cwtypes.MetricDataQuery{
		Id: awssdk.String("lat"),
		MetricStat: &cwtypes.MetricStat{
			Metric: &cwtypes.Metric{
				Namespace:  awssdk.String("AWS/ApplicationELB"),
				MetricName: awssdk.String("TargetResponseTime"),
				Dimensions: []cwtypes.Dimension{
					{Name: awssdk.String("LoadBalancer"), Value: awssdk.String(dimLB)},
				},
			},
			Period: awssdk.Int32(config.PeriodoPuntoSegundos),
			Stat:   awssdk.String("Average"),
		},
	})

	// Hosts saludables (HealthyHostCount) del Target Group: id "hosts".
	consultas = append(consultas, cwtypes.MetricDataQuery{
		Id: awssdk.String("hosts"),
		MetricStat: &cwtypes.MetricStat{
			Metric: &cwtypes.Metric{
				Namespace:  awssdk.String("AWS/ApplicationELB"),
				MetricName: awssdk.String("HealthyHostCount"),
				Dimensions: []cwtypes.Dimension{
					{Name: awssdk.String("TargetGroup"), Value: awssdk.String(dimTG)},
					{Name: awssdk.String("LoadBalancer"), Value: awssdk.String(dimLB)},
				},
			},
			Period: awssdk.Int32(config.PeriodoPuntoSegundos),
			Stat:   awssdk.String("Maximum"),
		},
	})

	entrada := &cloudwatch.GetMetricDataInput{
		StartTime:         awssdk.Time(inicio),
		EndTime:           awssdk.Time(ahora),
		MetricDataQueries: consultas,
		ScanBy:            cwtypes.ScanByTimestampDescending,
	}

	salida, err := ReintentarConValor(ctx, "GetMetricData", func() (*cloudwatch.GetMetricDataOutput, error) {
		return cCW.GetMetricData(ctx, entrada)
	})
	if err != nil {
		return policy.Snapshot{}, fmt.Errorf("fallo al consultar metricas en CloudWatch: %w", err)
	}

	// --- Procesar resultados ---
	var cpuMax float64
	var huboCPU bool
	var cpuMasReciente time.Time
	var latencia float64
	var hosts int

	for _, r := range salida.MetricDataResults {
		id := awssdk.ToString(r.Id)
		if len(r.Values) == 0 {
			continue
		}
		valorReciente := r.Values[0] // más reciente por ScanBy Descending

		switch {
		case strings.HasPrefix(id, "m"): // CPU de una instancia
			if valorReciente > cpuMax {
				cpuMax = valorReciente
			}
			huboCPU = true
			if r.Timestamps[0].After(cpuMasReciente) {
				cpuMasReciente = r.Timestamps[0]
			}
		case id == "lat":
			latencia = valorReciente
		case id == "hosts":
			hosts = int(valorReciente)
		}
	}

	// La confiabilidad se ancla en la CPU (métrica por instancia, la más
	// crítica y la que decide subir/bajar junto con la latencia).
	confiable := huboCPU && ahora.Sub(cpuMasReciente) <= config.MaxAntiguedadMetrica

	snap.CPUMax = cpuMax
	snap.LatenciaAvg = latencia
	snap.HostsSaludables = hosts
	snap.MetricaConfiable = confiable

	return snap, nil
}
