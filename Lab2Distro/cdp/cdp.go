package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"sync" // Añadir esta línea
	"time"

	pb "cdp/proto/grpc-server/proto" // Asegúrate de que el path sea correcto

	"github.com/streadway/amqp"
	"google.golang.org/grpc"
)

var claveAES = []byte("pokemonbattlesecretkey32bytelong")

// Estructura para los resultados de combate que se recibirán de RabbitMQ
type ResultadoCombateMsg struct {
	CombateID      string    `json:"combate_id"`
	TorneoID       string    `json:"torneo_id"`
	Entrenador1ID  string    `json:"entrenador1_id"`
	Entrenador2ID  string    `json:"entrenador2_id"`
	GanadorID      string    `json:"ganador_id"`
	FechaCombate   time.Time `json:"fecha_combate"`
	GimnasioNombre string    `json:"gimnasio_nombre"`
	Region         string    `json:"region"`
}

// Cliente gRPC para LCP (se mantiene para la verificación de entrenadores)
var lcpClient pb.LCPServiceClient

// Canal RabbitMQ para envío de resultados
var rabbitChannel *amqp.Channel

// Agregar esta variable global para almacenar IDs de combates ya procesados
var combatesProcesados = make(map[string]bool)
var mutexCombates = &sync.Mutex{}

// Función para conectar a gRPC
func conectarGRPC(address string, maxRetries int, retryDelay time.Duration) (*grpc.ClientConn, error) {
	var conn *grpc.ClientConn
	var err error
	//intentos
	for i := 0; i < maxRetries; i++ {
		conn, err = grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithTimeout(3*time.Second))
		if err == nil {
			return conn, nil
		}

		log.Printf("Intento %d: No se pudo conectar a %s: %v", i+1, address, err)
		time.Sleep(retryDelay)
	}

	return nil, fmt.Errorf("no se pudo conectar a %s después de %d intentos", address, maxRetries)
}

// Función para descifrar mensajes con AES-256
func descifrarAES(textoCifradoBase64 string) ([]byte, error) {
	// Decodificar
	textoCifrado, err := base64.StdEncoding.DecodeString(textoCifradoBase64)
	if err != nil {
		return nil, fmt.Errorf("error decodificando base64: %v", err)
	}

	// Cifrador AES
	bloque, err := aes.NewCipher(claveAES)
	if err != nil {
		return nil, err
	}

	// Extraer
	if len(textoCifrado) < 12 {
		return nil, fmt.Errorf("texto cifrado demasiado corto")
	}
	nonce := textoCifrado[:12]
	textoCifrado = textoCifrado[12:]

	// Crear GCM
	aead, err := cipher.NewGCM(bloque)
	if err != nil {
		return nil, err
	}

	// Descifrar
	textoPlano, err := aead.Open(nil, nonce, textoCifrado, nil)
	if err != nil {
		return nil, err
	}

	return textoPlano, nil
}

// Función para verificar si los entrenadores están registrados y activos
func verificarEntrenadores(entrenador1ID, entrenador2ID string) (bool, error) {
	if lcpClient == nil {
		return false, fmt.Errorf("no hay conexión establecida con LCP")
	}

	// Crear solicitud de verificación
	solicitud := &pb.VerificacionEntrenadores{
		EntrenadorIds: []string{entrenador1ID, entrenador2ID},
	}

	// Enviar solicitud al LCP
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resultado, err := lcpClient.VerificarEntrenadores(ctx, solicitud)
	if err != nil {
		return false, fmt.Errorf("error verificando entrenadores: %v", err)
	}

	return resultado.TodosValidos, nil
}

// Función para verificar si un mensaje tiene estructura correcta
func validarEstructuraMensaje(resultado *ResultadoCombateMsg) error {
	// Verificar IDs requeridos
	if resultado.CombateID == "" {
		return fmt.Errorf("mensaje inválido: combate_id vacío")
	}
	if resultado.TorneoID == "" {
		return fmt.Errorf("mensaje inválido: torneo_id vacío")
	}
	if resultado.Entrenador1ID == "" || resultado.Entrenador2ID == "" {
		return fmt.Errorf("mensaje inválido: IDs de entrenadores incompletos")
	}
	if resultado.GanadorID == "" {
		return fmt.Errorf("mensaje inválido: ganador_id vacío")
	}

	// Verificar que el ganador sea uno de los entrenadores
	if resultado.GanadorID != resultado.Entrenador1ID && resultado.GanadorID != resultado.Entrenador2ID {
		return fmt.Errorf("mensaje inválido: el ganador_id no corresponde a ninguno de los entrenadores")
	}

	// Verificar fecha de combate válida
	if resultado.FechaCombate.IsZero() {
		return fmt.Errorf("mensaje inválido: fecha_combate vacía o inválida")
	}

	// Verificar información de ubicación
	if resultado.GimnasioNombre == "" {
		return fmt.Errorf("mensaje inválido: gimnasio_nombre vacío")
	}

	return nil
}

// Función para verificar si es un mensaje duplicado
func esMensajeDuplicado(combateID string) bool {
	mutexCombates.Lock()
	defer mutexCombates.Unlock()

	// Verificar si ya procesamos este combate
	if combatesProcesados[combateID] {
		return true
	}

	// Registrar el combate como procesado
	combatesProcesados[combateID] = true
	return false
}

// Procesar mensaje recibido (actualizado para usar RabbitMQ)
func procesarMensaje(mensaje []byte, region string) error {
	// Descifrar mensaje
	textoPlano, err := descifrarAES(string(mensaje))
	if err != nil {
		return fmt.Errorf("error descifrando mensaje: %v", err)
	}

	// Decodificar JSON
	var resultado ResultadoCombateMsg
	if err := json.Unmarshal(textoPlano, &resultado); err != nil {
		return fmt.Errorf("error decodificando JSON: %v", err)
	}

	// Validar estructura del mensaje
	if err := validarEstructuraMensaje(&resultado); err != nil {
		return fmt.Errorf("validación de mensaje fallida: %v", err)
	}

	// Verificar si es un mensaje duplicado
	if esMensajeDuplicado(resultado.CombateID) {
		log.Printf("ADVERTENCIA: Mensaje duplicado detectado para combate ID: %s. Ignorando.", resultado.CombateID)
		return fmt.Errorf("mensaje duplicado: combate %s ya fue procesado", resultado.CombateID)
	}

	// Verificar que los entrenadores estén registrados y activos
	validos, err := verificarEntrenadores(resultado.Entrenador1ID, resultado.Entrenador2ID)
	if err != nil {
		log.Printf("ADVERTENCIA: Error verificando estado de entrenadores: %v. Procesando de todos modos.", err)
	} else if !validos {
		log.Printf("ADVERTENCIA: Uno o ambos entrenadores no están registrados o activos. Procesando de todos modos.")
	}

	// Imprimir resultado formateado usando log.Printf en vez de fmt.Printf
	log.Printf("")
	log.Printf("")
	log.Printf("=== Resultado de Combate ===")
	log.Printf("Región: %s", region)
	log.Printf("Gimnasio: %s", resultado.GimnasioNombre)
	log.Printf("Torneo ID: %s", resultado.TorneoID)
	log.Printf("Combate ID: %s", resultado.CombateID)
	log.Printf("Entrenador 1: %s", resultado.Entrenador1ID)
	log.Printf("Entrenador 2: %s", resultado.Entrenador2ID)
	log.Printf("¡Ganador: %s!", resultado.GanadorID)
	log.Printf("Fecha: %s", resultado.FechaCombate.Format("2006-01-02 15:04:05"))
	log.Printf("===========================")

	// Enviar el resultado a LCP a través de RabbitMQ en lugar de gRPC
	if rabbitChannel != nil {
		// Convertir el resultado a JSON
		resultadoJSON, err := json.Marshal(resultado)
		if err != nil {
			return fmt.Errorf("error codificando resultado a JSON: %v", err)
		}

		// Publicar en la cola de RabbitMQ
		err = rabbitChannel.Publish(
			"",                    // exchange
			"resultados_combates", // routing key (nombre de la cola)
			false,                 // mandatory
			false,                 // immediate
			amqp.Publishing{
				ContentType: "application/json",
				Body:        resultadoJSON,
			})

		if err != nil {
			log.Printf("Error enviando resultado a RabbitMQ: %v", err)
		} else {
			log.Printf("Resultado enviado a LCP mediante RabbitMQ: combate %s", resultado.CombateID)
		}
	} else {
		log.Printf("No hay canal RabbitMQ disponible para enviar resultado")
	}

	return nil
}

// Función para conectar a RabbitMQ con reintentos
func conectarRabbitMQ(maxRetries int, retryDelay time.Duration) (*amqp.Connection, *amqp.Channel, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		// Usuario y contraseña de RabbitMQ
		conn, err := amqp.Dial("amqp://pokemon:pokemon123@10.35.168.63:5672/")
		if err == nil {
			ch, err := conn.Channel()
			if err == nil {
				return conn, ch, nil
			}
			conn.Close()
			lastErr = err
		} else {
			lastErr = err
		}

		log.Printf("Intento %d: Error conectando a RabbitMQ: %v. Reintentando en %v...", i+1, err, retryDelay)
		time.Sleep(retryDelay)
	}
	return nil, nil, fmt.Errorf("no se pudo conectar después de %d intentos: %v", maxRetries, lastErr)
}

func main() {
	log.Println("Iniciando Combat Data Processor (CDP)...")

	// Conectar a LCP con gRPC
	connLCP, err := conectarGRPC("10.35.168.63:50051", 5, time.Second*3)
	if err != nil {
		log.Printf("Error conectando con LCP: %v. Continuando sin verificar entrenadores.", err)
	} else {
		log.Println("Conexión establecida con LCP")
		lcpClient = pb.NewLCPServiceClient(connLCP)
		defer connLCP.Close()
	}

	// Conectar a RabbitMQ
	rabbitConn, ch, err := conectarRabbitMQ(5, time.Second*3)
	if err != nil {
		log.Fatalf("Error conectando a RabbitMQ: %v", err)
	}
	defer rabbitConn.Close()
	defer ch.Close()

	// Canal para envío de resultados
	rabbitChannel = ch

	// Cola para resultados (asegurar que exista)
	_, err = ch.QueueDeclare(
		"resultados_combates", // nombre
		false,                 // durable
		false,                 // delete when unused
		false,                 // exclusive
		false,                 // no-wait
		nil,                   // arguments
	)
	if err != nil {
		log.Fatalf("Error declarando cola de resultados: %v", err)
	}
	log.Println("Cola de resultados configurada correctamente")

	// Lista de regiones para monitorear
	regiones := []string{"kanto", "johto", "hoenn", "sinnoh", "unova"}

	// Crear un canal para recibir mensajes de todas las colas
	mensajes := make(chan struct {
		contenido []byte
		region    string
	})

	// Configurar consumidor para cada región
	for _, region := range regiones {
		queueName := "combates_" + region

		// Asegurarse de que la cola existe
		_, err := ch.QueueDeclare(
			queueName,
			false,
			false,
			false,
			false,
			nil,
		)
		if err != nil {
			log.Printf("Error declarando cola %s: %v", queueName, err)
			continue
		}

		// Crear consumidor
		msgs, err := ch.Consume(
			queueName,
			"",
			true,
			false,
			false,
			false,
			nil,
		)
		if err != nil {
			log.Printf("Error creando consumidor para %s: %v", queueName, err)
			continue
		}

		// Procesar mensajes de esta región
		go func(regionName string, delivery <-chan amqp.Delivery) {
			for d := range delivery {
				mensajes <- struct {
					contenido []byte
					region    string
				}{d.Body, regionName}
			}
		}(region, msgs)

		log.Printf("Escuchando combates de la región: %s", region)
	}

	log.Println("CDP listo para procesar resultados de combates...")

	// Procesar mensajes recibidos
	for msg := range mensajes {
		if err := procesarMensaje(msg.contenido, msg.region); err != nil {
			log.Printf("Error procesando mensaje de %s: %v", msg.region, err)
		}
	}
}
