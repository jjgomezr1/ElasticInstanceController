// Package config centraliza todos los umbrales, cooldowns y constantes del
// controlador de auto-escalado. Es la única fuente de verdad: cualquier otra
// pieza que necesite un valor de política lo importa desde aquí, de modo que
// un cambio de umbral se haga en un solo lugar.
package config

import (
	"os"
	"time"
)

// getenv devuelve el valor de una variable de entorno, o un valor por defecto
// si no está definida. Permite sobreescribir parámetros de despliegue (AMI,
// tipo, key, security group) sin recompilar el binario.
func getenv(clave, porDefecto string) string {
	if v := os.Getenv(clave); v != "" {
		return v
	}
	return porDefecto
}

// --- Límites de capacidad (impuestos por el reto) ---

const (
	// MinInstancias es la cantidad mínima de instancias que el controlador
	// debe mantener corriendo en todo momento.
	MinInstancias = 1
	// MaxInstancias es el tope superior de instancias permitido por el reto.
	MaxInstancias = 5
)

// --- Umbrales de decisión ---
//
// Política final (fase ALB), con histéresis entre subir y bajar para evitar
// oscilación:
//   Subir  = (CPU > CPUSubir  O  latencia > LatenciaSubir)  Y  instancias < max
//   Bajar  =  CPU < CPUBajar  Y  latencia < LatenciaBajar
//            Y  hosts_sanos == instancias  Y  instancias > min
//
// El umbral de subida de CPU es 60% (no 70%) por la naturaleza "burstable" de
// la t3.micro: su baseline sostenible es ~10-20%, así que conviene escalar
// antes de que se agoten los créditos de CPU.

const (
	// CPUSubir: CPU máxima por encima de este valor empuja a subir.
	CPUSubir = 60.0
	// CPUBajar: CPU máxima por debajo de este valor habilita bajar.
	CPUBajar = 30.0

	// LatenciaSubir: TargetResponseTime (segundos) por encima de este valor
	// empuja a subir (el usuario está esperando demasiado).
	LatenciaSubir = 1.0
	// LatenciaBajar: TargetResponseTime por debajo de este valor es condición
	// (entre otras) para poder bajar. La banda 0.4-1.0s es zona muerta.
	LatenciaBajar = 0.4
)

// --- Ventana de observación y consulta de métricas ---

const (
	// VentanaObservacion es el periodo hacia atrás que se mira para decidir.
	// Punto medio entre reaccionar a picos aislados y tardar demasiado.
	VentanaObservacion = 4 * time.Minute

	// PeriodoPuntoSegundos es la granularidad de cada punto que se pide a
	// CloudWatch, en segundos. 60s coincide con el ritmo de publicación del
	// monitoreo detallado de EC2, y hace que dentro de la ventana haya varios
	// puntos con timestamps recientes (evita el problema de un único bucket
	// grande cuyo timestamp quedaría siempre por fuera del límite de
	// antigüedad).
	PeriodoPuntoSegundos = 60

	// MaxAntiguedadMetrica: si el dato más reciente de una métrica es más
	// viejo que esto, se considera no confiable y el controlador mantiene el
	// estado actual sin decidir en falso.
	MaxAntiguedadMetrica = 2 * time.Minute
)

// --- Cooldowns (anti-oscilación) ---
//
// El cooldown de bajada es mayor que el de subida porque el
// under-provisioning afecta al usuario de inmediato, mientras que el
// over-provisioning no.

const (
	// CooldownSubida se cuenta desde que la instancia nueva queda con
	// "capacidad efectiva" (pasa el status check), no desde que se lanzó.
	CooldownSubida = 3 * time.Minute
	// CooldownBajada se cuenta desde que la instancia terminada se confirma
	// terminada.
	CooldownBajada = 6 * time.Minute
)

// --- Draining (bajada segura con ALB) ---
//
// Al bajar: desregistrar el target -> esperar el draining -> terminar. El
// deregistration delay del Target Group está configurado en 30s (la app
// responde en milisegundos, no necesita más). Sondeamos el estado del target
// hasta que deje de estar "draining", con un tope de espera por seguridad.

const (
	// EsperaMaxDraining es el tope de tiempo que esperamos a que un target
	// salga del estado "draining" antes de terminar de todos modos.
	EsperaMaxDraining = 45 * time.Second
	// IntervaloSondeoDraining es cada cuánto consultamos el estado del target
	// durante el draining.
	IntervaloSondeoDraining = 5 * time.Second
)

// --- Ritmo del ciclo principal y reintentos ante fallos de AWS ---

const (
	// IntervaloEvaluacion es cada cuánto corre el ciclo completo del
	// controlador (inventario -> métricas -> decidir -> actuar -> loguear).
	IntervaloEvaluacion = 1 * time.Minute

	// MaxReintentos es la cantidad de reintentos ante un fallo de AWS antes
	// de abortar la acción y mantener el estado actual.
	MaxReintentos = 2
	// EsperaEntreReintentos es la pausa entre reintentos.
	EsperaEntreReintentos = 20 * time.Second
)

// --- Etiquetado de instancias gestionadas ---
//
// Al no usar un Auto Scaling Group, el "agrupamiento" de nuestras instancias
// se hace con un tag. La Pieza 1 (inventario) busca exactamente este tag.

const (
	// TagClave es la clave del tag que marca una instancia como gestionada
	// por este controlador.
	TagClave = "ManagedBy"
	// TagValor es el valor esperado del tag.
	TagValor = "autoscaling-controller"
)

// --- Parámetros de infraestructura (fase ALB) ---
//
// Son valores propios de la cuenta/región/despliegue. Tienen un valor por
// defecto acorde al entorno actual, pero se pueden sobreescribir por variable
// de entorno sin recompilar (recomendado, porque estos IDs cambian si se
// recrea la infraestructura).
var (
	// TipoInstancia es el tipo EC2 de las instancias sujeto que se lanzan.
	TipoInstancia = getenv("TIPO_INSTANCIA", "t3.micro")

	// KeyPair es el par de claves para poder entrar por SSH a las instancias.
	KeyPair = getenv("KEY_PAIR", "vockey")

	// AMIImagen es la AMI DORADA con la app inversora ya instalada como
	// servicio (arranca sola al bootear). El controlador lanza copias de esta
	// imagen; así cada instancia nueva queda lista para el ALB sin
	// intervención. Reemplaza al antiguo parámetro SSM de Ubuntu vacía.
	AMIImagen = getenv("AMI_ID", "ami-01c0739a01c678a19")

	// SubnetID es la subred (pública, en la VPC del proyecto) donde se lanzan
	// las instancias sujeto. Debe estar en la misma VPC que el ALB.
	SubnetID = getenv("SUBNET_ID", "subnet-036fdfb4a9105016f")

	// SecurityGroupID es el grupo de seguridad de las instancias sujeto
	// (sg-instancias): solo acepta HTTP desde el ALB, y SSH para depurar.
	SecurityGroupID = getenv("SECURITY_GROUP", "sg-09da9e06537de936d")

	// TargetGroupARN es el ARN del Target Group del ALB. El controlador
	// registra ahí las instancias que lanza y las desregistra antes de
	// terminarlas.
	TargetGroupARN = getenv(
		"TARGET_GROUP_ARN",
		"arn:aws:elasticloadbalancing:us-east-1:361114871808:targetgroup/tg-InversorApp/bb56724fea044efd",
	)

	// ALBNombre es el nombre del Application Load Balancer, usado como
	// dimensión al consultar TargetResponseTime en CloudWatch.
	ALBNombre = getenv("ALB_NOMBRE", "alb-InversorApp")
)
