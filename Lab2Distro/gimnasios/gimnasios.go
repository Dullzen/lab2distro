package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"time"

	pb "gimnasios/proto/grpc-server/proto"

	"github.com/streadway/amqp"
	"google.golang.org/grpc"
)

// Estructura para los gimnasios Pokemon
type Gimnasio struct {
	Nombre string
	Region string
}

// Clave compartida para descifrado AES-256 (32 bytes)
// Clave compartida para descifrado AES-256 (32 bytes)
var claveAES = []byte("pokemonbattlesecretkey32bytelong")

// Estructura para el servidor
type server struct {
	pb.UnimplementedLCPServiceServer
	rabbitConn    *amqp.Connection
	rabbitChannel *amqp.Channel
	queues        map[string]amqp.Queue
}

// Estructura para los resultados de combate que se enviarán a RabbitMQ
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

// SimularCombate simula un combate entre dos entrenadores y devuelve el ID del ganador
func SimularCombate(ent1, ent2 *pb.Entrenador) string {
	// Asegurar que los rankings tengan valores razonables
	ranking1 := ent1.Ranking
	if ranking1 <= 0 {
		ranking1 = 100 // Valor por defecto
	}

	ranking2 := ent2.Ranking
	if ranking2 <= 0 {
		ranking2 = 100 // Valor por defecto
	}

	diff := ranking1 - ranking2

	if diff > 300 {
		return ent1.Id
	}
	if diff < -300 {
		return ent2.Id
	}

	// Usar los rankings para determinar probabilidad
	totalRanking := ranking1 + ranking2
	if totalRanking <= 0 { // Por si acaso hay rankings negativos
		// 50/50 chance
		if rand.Intn(2) == 0 {
			return ent1.Id
		} else {
			return ent2.Id
		}
	} else {
		if rand.Intn(int(totalRanking)) < int(ranking1) {
			return ent1.Id
		}
	}
	return ent2.Id
}

// Función para cifrar mensajes con AES-256
func cifrarAES(textoPlano []byte) (string, error) {
	bloque, err := aes.NewCipher(claveAES)
	if err != nil {
		return "", err
	}

	// Crear un nonce aleatorio
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	// Crear GCM (Galois/Counter Mode)
	aead, err := cipher.NewGCM(bloque)
	if err != nil {
		return "", err
	}

	// Cifrar el texto
	textoCifrado := aead.Seal(nil, nonce, textoPlano, nil)

	// Combinar nonce y texto cifrado
	resultado := make([]byte, len(nonce)+len(textoCifrado))
	copy(resultado, nonce)
	copy(resultado[len(nonce):], textoCifrado)

	// Codificar en base64 para transmisión segura
	return base64.StdEncoding.EncodeToString(resultado), nil
}

// Función para conectar a RabbitMQ con reintentos
func conectarRabbitMQ(maxRetries int, retryDelay time.Duration) (*amqp.Connection, *amqp.Channel, map[string]amqp.Queue, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		conn, err := amqp.Dial("amqp://guest:guest@10.35.168.63:5672/") // Cambiado a la segunda VM donde estará RabbitMQ
		if err == nil {
			ch, err := conn.Channel()
			if err == nil {
				// Crear colas
				queues := make(map[string]amqp.Queue)
				regiones := []string{"kanto", "johto", "hoenn", "sinnoh", "unova"}

				for _, region := range regiones {
					q, err := ch.QueueDeclare(
						"combates_"+region,
						false,
						false,
						false,
						false,
						nil,
					)
					if err != nil {
						ch.Close()
						conn.Close()
						return nil, nil, nil, fmt.Errorf("error creando cola para %s: %v", region, err)
					}
					queues[region] = q
				}

				return conn, ch, queues, nil
			}
			conn.Close()
			lastErr = err
		} else {
			lastErr = err
		}

		log.Printf("Intento %d: Error conectando a RabbitMQ: %v. Reintentando en %v...", i+1, err, retryDelay)
		time.Sleep(retryDelay)
	}
	return nil, nil, nil, fmt.Errorf("no se pudo conectar después de %d intentos: %v", maxRetries, lastErr)
}

// Publicar resultado de combate en RabbitMQ con reintentos
func (s *server) publicarResultadoCombate(resultado ResultadoCombateMsg) error {
	if s.rabbitChannel == nil {
		return fmt.Errorf("canal RabbitMQ no inicializado")
	}

	// Convertir a JSON
	jsonData, err := json.Marshal(resultado)
	if err != nil {
		return fmt.Errorf("error al convertir resultado a JSON: %v", err)
	}

	// Cifrar el mensaje
	mensajeCifrado, err := cifrarAES(jsonData)
	if err != nil {
		return fmt.Errorf("error al cifrar mensaje: %v", err)
	}

	// Publicar en la cola correspondiente
	queueName := "combates_" + resultado.Region
	queue, exists := s.queues[resultado.Region]
	if !exists {
		return fmt.Errorf("no existe cola para región %s", resultado.Region)
	}

	// Implementación de reintentos (3 intentos)
	maxRetries := 3
	var lastErr error
	for intento := 0; intento < maxRetries; intento++ {
		err = s.rabbitChannel.Publish(
			"",         // exchange
			queue.Name, // routing key
			false,      // mandatory
			false,      // immediate
			amqp.Publishing{
				ContentType: "text/plain",
				Body:        []byte(mensajeCifrado),
			})

		if err == nil {
			log.Printf("Resultado de combate publicado en cola %s (intento %d)", queueName, intento+1)
			return nil // Éxito, salimos del bucle
		}

		lastErr = err
		log.Printf("Error al publicar resultado en RabbitMQ (intento %d): %v", intento+1, err)
		time.Sleep(2 * time.Second) // Esperar antes de reintentar
	}

	// Si llegamos aquí, fallaron todos los intentos
	errMsg := fmt.Sprintf("[%s] ERROR: Fallo al publicar resultado de combate después de %d intentos: %v. Combate: %s, Torneo: %s, Gimnasio: %s (%s)",
		time.Now().Format("2006-01-02 15:04:05"),
		maxRetries,
		lastErr,
		resultado.CombateID,
		resultado.TorneoID,
		resultado.GimnasioNombre,
		resultado.Region)

	// Registrar el error en el archivo log_errores.txt
	registrarErrorLog(errMsg)

	return fmt.Errorf("error al publicar mensaje después de %d intentos: %v", maxRetries, lastErr)
}

// Función para registrar errores en archivo log_errores.txt
func registrarErrorLog(mensaje string) {
	// Asegurar que el directorio logs existe
	logDir := "logs"
	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		err = os.MkdirAll(logDir, 0755)
		if err != nil {
			log.Printf("Error al crear directorio de logs: %v", err)
			return
		}
	}

	logFile := filepath.Join(logDir, "log_errores.txt")

	// Abrir el archivo en modo append o crearlo si no existe
	file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error al abrir archivo de log: %v", err)
		return
	}
	defer file.Close()

	// Escribir el mensaje de error con una nueva línea
	if _, err := file.WriteString(mensaje + "\n"); err != nil {
		log.Printf("Error al escribir en archivo de log: %v", err)
	}
}

// AsignarCombate implementa el método gRPC para recibir y procesar solicitudes de combate
func (s *server) AsignarCombate(ctx context.Context, in *pb.SolicitudCombate) (*pb.ResultadoCombate, error) {
	combateID := in.CombateId
	torneoID := in.TorneoId
	log.Printf("")
	log.Printf("")
	// 1. Log de recepción de solicitud con formato consistente
	log.Printf("[Solicitud] Combate %s del torneo %s: %s vs %s en gimnasio %s (%s)",
		combateID, torneoID, in.Entrenador1Id, in.Entrenador2Id,
		in.GimnasioNombre, in.GimnasioRegion)

	// Crear objetos Entrenador temporales basados en las IDs y USAR LOS RANKINGS REALES
	entrenador1 := &pb.Entrenador{
		Id:      in.Entrenador1Id,
		Ranking: in.Entrenador1Ranking, // Usar ranking real recibido
	}

	entrenador2 := &pb.Entrenador{
		Id:      in.Entrenador2Id,
		Ranking: in.Entrenador2Ranking, // Usar ranking real recibido
	}

	// 2. Log de simulación de combate con información de rankings
	log.Printf("[Procesando] Combate %s: Enfrentamiento %s (%s) - Rankings: %d vs %d",
		combateID, in.GimnasioNombre, in.GimnasioRegion,
		entrenador1.Ranking, entrenador2.Ranking)

	// Simular el combate
	idGanador := SimularCombate(entrenador1, entrenador2)

	// Usar el gimnasio recibido en la solicitud
	gimnasio := Gimnasio{
		Nombre: in.GimnasioNombre,
		Region: in.GimnasioRegion,
	}

	// 3. Log del resultado del combate
	log.Printf("[Resultado] Combate %s: Ganador: %s (Enfrentamiento: %s vs %s)",
		combateID, idGanador, in.Entrenador1Id, in.Entrenador2Id)

	// Preparar resultado para RabbitMQ
	resultado := ResultadoCombateMsg{
		CombateID:      combateID,
		TorneoID:       torneoID,
		Entrenador1ID:  in.Entrenador1Id,
		Entrenador2ID:  in.Entrenador2Id,
		GanadorID:      idGanador,
		FechaCombate:   time.Now(),
		GimnasioNombre: gimnasio.Nombre,
		Region:         gimnasio.Region,
	}

	// 4. Log de publicación - inicio
	log.Printf("[Publicando] Combate %s: Enviando resultado a cola RabbitMQ '%s'",
		combateID, "combates_"+gimnasio.Region)

	// Intentar publicar el resultado
	err := s.publicarResultadoCombate(resultado)

	// 5. Log de publicación - resultado
	if err != nil {
		log.Printf("[Error] Combate %s: Fallo al publicar en RabbitMQ: %v", combateID, err)
		// A pesar del error, continuamos con la respuesta al cliente
	} else {
		log.Printf("[Éxito] Combate %s: Resultado publicado correctamente en cola de %s",
			combateID, gimnasio.Region)
	}

	// Devolver solo confirmación (sin ganador)
	return &pb.ResultadoCombate{
		CombateId:     combateID,
		TorneoId:      torneoID,
		GanadorId:     "", // No enviamos el ganador aquí
		GanadorNombre: "",
		Error:         false,
		Mensaje:       "Enviando resultado a CDP.",
	}, nil
}

func main() {
	// Inicializar el generador de números aleatorios
	rand.Seed(time.Now().UnixNano())

	// Crear el servidor
	s := &server{
		rabbitConn:    nil,
		rabbitChannel: nil,
		queues:        nil,
	}

	// PRIMERO iniciar el servidor gRPC
	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatalf("Fallo al escuchar: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterLCPServiceServer(grpcServer, s)

	log.Println("Servidor de Gimnasios corriendo en puerto 50052")

	// Iniciar el servidor en una goroutine
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Fallo al iniciar servidor: %v", err)
		}
	}()

	// Pequeña pausa para asegurar que el servidor está escuchando
	log.Println("Esperando a que el servidor gRPC inicie completamente...")
	time.Sleep(2 * time.Second)

	// DESPUÉS intentar inicializar RabbitMQ
	rabbitConn, rabbitChannel, queues, err := conectarRabbitMQ(10, 5*time.Second)
	if err != nil {
		log.Printf("Advertencia: No se pudo inicializar RabbitMQ: %v", err)
		log.Println("El servidor continuará funcionando sin RabbitMQ")
	} else {
		log.Println("Conexión a RabbitMQ establecida correctamente")
		defer rabbitConn.Close()
		defer rabbitChannel.Close()

		// Actualizar el servidor con las conexiones establecidas
		s.rabbitConn = rabbitConn
		s.rabbitChannel = rabbitChannel
		s.queues = queues
	}

	// Bloquear el programa para que no termine
	select {}
}
