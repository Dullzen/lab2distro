package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	pb "lcp/proto/grpc-server/proto"

	// Add RabbitMQ library
	"github.com/streadway/amqp"
	"google.golang.org/grpc"
)

// ResultadoCombateMQ represents the message structure for combat results from RabbitMQ
type ResultadoCombateMQ struct {
	CombateID      string    `json:"combate_id"`
	TorneoID       string    `json:"torneo_id"`
	Entrenador1ID  string    `json:"entrenador1_id"`
	Entrenador2ID  string    `json:"entrenador2_id"`
	GanadorID      string    `json:"ganador_id"`
	FechaCombate   time.Time `json:"fecha_combate"`
	GimnasioNombre string    `json:"gimnasio_nombre"`
	Region         string    `json:"region"`
}

var (
	contadorTorneos int        = 0 // Contador global para numerar los torneos
	contadorMutex   sync.Mutex     // Mutex para proteger el acceso al contador
)

// Estructura para representar un torneo
type Torneo struct {
	TorneoID      string // Nuevo campo para el ID numérico
	Nombre        string
	Participantes []*pb.Entrenador
	Ganador       *pb.Entrenador
	FechaInicio   time.Time
	Estado        string // "inscripcion", "en_curso", "finalizado"
}

// Estructura para los gimnasios Pokemon
type Gimnasio struct {
	Nombre string
	Region string
}

// Lista de gimnasios por región
var gimnasiosPorRegion = map[string][]Gimnasio{
	"kanto": {
		{Nombre: "Gimnasio de Ciudad Plateada", Region: "kanto"},
		{Nombre: "Gimnasio de Ciudad Celeste", Region: "kanto"},
		{Nombre: "Gimnasio de Ciudad Carmín", Region: "kanto"},
	},
	"johto": {
		{Nombre: "Gimnasio de Ciudad Malva", Region: "johto"},
		{Nombre: "Gimnasio de Ciudad Azalea", Region: "johto"},
		{Nombre: "Gimnasio de Ciudad Iris", Region: "johto"},
	},
	"hoenn": {
		{Nombre: "Gimnasio de Ciudad Petalia", Region: "hoenn"},
		{Nombre: "Gimnasio de Ciudad Férrica", Region: "hoenn"},
		{Nombre: "Gimnasio de Ciudad Malvalona", Region: "hoenn"},
	},
	"sinnoh": {
		{Nombre: "Gimnasio de Ciudad Pirita", Region: "sinnoh"},
		{Nombre: "Gimnasio de Ciudad Vetusta", Region: "sinnoh"},
		{Nombre: "Gimnasio de Ciudad Veilstone", Region: "sinnoh"},
	},
	"unova": {
		{Nombre: "Gimnasio de Ciudad Straiton", Region: "unova"},
		{Nombre: "Gimnasio de Ciudad Nacrene", Region: "unova"},
		{Nombre: "Gimnasio de Ciudad Castelia", Region: "unova"},
	},
}

// Función para seleccionar un gimnasio aleatorio
func seleccionarGimnasio() Gimnasio {
	// Obtener todas las regiones
	regiones := make([]string, 0, len(gimnasiosPorRegion))
	for region := range gimnasiosPorRegion {
		regiones = append(regiones, region)
	}

	// Seleccionar región aleatoria
	regionSeleccionada := regiones[rand.Intn(len(regiones))]

	// Seleccionar gimnasio aleatorio de esa región
	gimnasios := gimnasiosPorRegion[regionSeleccionada]
	gimnasioSeleccionado := gimnasios[rand.Intn(len(gimnasios))]

	return gimnasioSeleccionado
}

type server struct {
	pb.UnimplementedLCPServiceServer
	entrenadoresMap       map[string]*pb.Entrenador
	torneos               map[string]*Torneo
	mu                    sync.Mutex
	combatesPendientes    map[string]chan string // Mapeo de combateID -> canal para resultado
	combatesProcesados    map[string]bool        // Para detectar mensajes duplicados
	resultadosAdelantados map[string]string      // Resultados de combates que llegaron antes de tiempo
	ultimaProcesamiento   sync.Mutex             // Para proteger el acceso al mapa de mensajes procesados
}

func (s *server) ObtenerListaEntrenadores(ctx context.Context, in *pb.Empty) (*pb.ListaEntrenadores, error) {
	entrenadores := make([]*pb.Entrenador, 0, len(s.entrenadoresMap))
	for _, e := range s.entrenadoresMap {
		entrenadores = append(entrenadores, e)
	}
	return &pb.ListaEntrenadores{Entrenadores: entrenadores}, nil
}

// Método para obtener lista de torneos
func (s *server) ObtenerTorneos(ctx context.Context, in *pb.Empty) (*pb.ListaTorneos, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	torneos := make([]*pb.InfoTorneo, 0, len(s.torneos))
	for _, t := range s.torneos {
		torneoInfo := &pb.InfoTorneo{
			Nombre:   t.Nombre,
			Estado:   t.Estado,
			TorneoId: t.TorneoID, // Añadir el ID del torneo al mensaje
		}
		if t.Ganador != nil {
			torneoInfo.GanadorId = t.Ganador.Id
			torneoInfo.GanadorNombre = t.Ganador.Nombre
		}
		torneos = append(torneos, torneoInfo)
	}

	return &pb.ListaTorneos{Torneos: torneos}, nil
}

// Método para crear un nuevo torneo
func (s *server) crearTorneo() *Torneo {
	nombreTorneo, torneoID := generarNombreTorneo()

	s.mu.Lock()
	nuevoTorneo := &Torneo{
		TorneoID:      torneoID,
		Nombre:        nombreTorneo,
		Participantes: make([]*pb.Entrenador, 0),
		FechaInicio:   time.Now(),
		Estado:        "inscripcion",
	}
	s.torneos[torneoID] = nuevoTorneo // Usar TorneoID como clave del mapa
	s.mu.Unlock()

	return nuevoTorneo
}

// Método para inscribir un entrenador al torneo actual
func (s *server) InscribirEntrenador(ctx context.Context, in *pb.SolicitudInscripcion) (*pb.ResultadoOperacion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Buscar el torneo actual en estado de inscripción
	var torneoActual *Torneo
	for _, t := range s.torneos {
		if t.Estado == "inscripcion" {
			torneoActual = t
			break
		}
	}

	if torneoActual == nil {
		return &pb.ResultadoOperacion{
			Exito:   false,
			Mensaje: "No hay torneos abiertos para inscripción actualmente",
		}, nil
	}

	// Verificar si el entrenador ya existe en el sistema
	entrenador, existe := s.entrenadoresMap[in.EntrenadorId]

	// Si el entrenador no existe, crear un nuevo entrenador
	if !existe {
		// Crear un nuevo entrenador con los datos proporcionados
		entrenador = &pb.Entrenador{
			Id:      in.EntrenadorId,
			Nombre:  in.EntrenadorNombre, // Asumir que estos campos se añadirán al mensaje SolicitudInscripcion
			Region:  in.EntrenadorRegion, // en el archivo proto
			Ranking: in.EntrenadorRanking,
			Estado:  "activo",
		}
		s.entrenadoresMap[in.EntrenadorId] = entrenador
	}
	log.Printf("Inscrito: Entrenador %s (ID: %s)", entrenador.Nombre, entrenador.Id)
	// Verificar si ya está inscrito en este torneo específico
	for _, p := range torneoActual.Participantes {
		if p.Id == entrenador.Id {
			return &pb.ResultadoOperacion{
				Exito:   false,
				Mensaje: "El entrenador ya está inscrito en este torneo",
			}, nil
		}
	}

	// Inscribir al entrenador en el torneo
	torneoActual.Participantes = append(torneoActual.Participantes, entrenador)

	return &pb.ResultadoOperacion{
		Exito: true,
		Mensaje: fmt.Sprintf("Entrenador %s inscrito con éxito en el torneo %s (ID: %s)",
			entrenador.Nombre, torneoActual.Nombre, torneoActual.TorneoID),
	}, nil
}

func conectarGRPC(address string, maxRetries int, retryDelay time.Duration) (*grpc.ClientConn, error) {
	var conn *grpc.ClientConn
	var err error

	for i := 0; i < maxRetries; i++ {
		conn, err = grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithTimeout(3*time.Second))
		if err == nil {
			return conn, nil // Conexión exitosa
		}

		log.Printf("Intento %d: No se pudo conectar a %s: %v", i+1, address, err)
		time.Sleep(retryDelay) // Esperar antes de reintentar
	}

	return nil, fmt.Errorf("no se pudo conectar a %s después de %d intentos", address, maxRetries)
}

// Generar nombre de torneo limpio y ID separado
func generarNombreTorneo() (string, string) {
	contadorMutex.Lock()
	defer contadorMutex.Unlock()

	contadorTorneos++
	torneoID := fmt.Sprintf("T%03d", contadorTorneos)

	tematicas := []string{
		"Pokémon", "Maestros", "Campeones", "Entrenadores",
	}

	tematica := tematicas[rand.Intn(len(tematicas))]
	nombreTorneo := fmt.Sprintf("Torneo %s", tematica)

	return nombreTorneo, torneoID
}

func main() {
	entrenadoresMap := make(map[string]*pb.Entrenador)

	// Crear el servidor una sola vez, con todos los campos inicializados
	s := &server{
		entrenadoresMap:       entrenadoresMap,
		torneos:               make(map[string]*Torneo),
		combatesPendientes:    make(map[string]chan string),
		combatesProcesados:    make(map[string]bool),
		resultadosAdelantados: make(map[string]string),
	}

	// Conectar a RabbitMQ para recibir resultados de combates
	rabbitConn, err := conectarRabbitMQ("amqp://guest:guest@localhost:5672/", 5, time.Second*3) // Usando localhost porque RabbitMQ estará en la misma VM
	if err != nil {
		log.Printf("Error conectando con RabbitMQ: %v", err)
		log.Println("Se usarán ganadores aleatorios para los combates")
	} else {
		log.Println("Conexión establecida con RabbitMQ")

		// Crear canal
		ch, err := rabbitConn.Channel()
		if err != nil {
			log.Printf("Error creando canal RabbitMQ: %v", err)
		} else {
			// Inicia el consumidor de resultados de combate
			go s.procesarResultadosCombates(ch, "resultados_combates")
			defer ch.Close()
		}

		defer rabbitConn.Close()
	}

	// Establecer conexión con gimnasios desde el inicio
	gimnasiosConn, err := conectarGRPC("10.35.168.64:50052", 5, time.Second) // Cambiado a la IP de la primera VM
	var gimnasiosClient pb.LCPServiceClient

	if err != nil {
		log.Printf("Error conectando con el servidor de gimnasios: %v", err)
		log.Println("Los combates utilizarán ganadores aleatorios")
	} else {
		gimnasiosClient = pb.NewLCPServiceClient(gimnasiosConn)
		log.Println("Conexión establecida con el servidor de gimnasios")
		// Cierra la conexión cuando termine el programa
		defer gimnasiosConn.Close()
	}

	// Iniciar el servidor gRPC - NO REINICIALIZAR EL SERVIDOR AQUÍ
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Fallo al escuchar: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterLCPServiceServer(grpcServer, s)

	// Iniciar el servidor en un goroutine para que no bloquee
	go func() {
		log.Println("LCP corriendo en puerto 50051")
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("Fallo al iniciar servidor: %v", err)
		}
	}()
	// Pequeña pausa para que el mensaje del servidor no se mezcle con los logs del torneo
	time.Sleep(1 * time.Second)
	// Generar torneos periódicamente
	go func() {

		for {
			// Crear nuevo torneo
			torneo := s.crearTorneo()
			log.Println("****************************************************************************************")
			log.Printf("¡Comienza el torneo: %s (ID: %s)!", torneo.Nombre, torneo.TorneoID)
			log.Println("Período de inscripción: 10 segundos")

			log.Println("Esperando inscripciones para el torneo...")

			// Continuar esperando por posibles inscripciones del usuario real
			inscripcionTimer := time.NewTimer(10 * time.Second)
			<-inscripcionTimer.C

			s.mu.Lock()
			// Cambiar estado a en curso
			torneo.Estado = "en_curso"
			participantes := len(torneo.Participantes)
			s.mu.Unlock()
			log.Println("________________________________________________________________________________________")
			log.Printf("¡Las inscripciones para %s (ID: %s) han cerrado!", torneo.Nombre, torneo.TorneoID)

			if participantes == 0 {
				log.Printf("No se recibieron inscripciones para el torneo %s. Cancelando torneo.", torneo.Nombre)
				s.mu.Lock()
				torneo.Estado = "cancelado" // Nuevo estado para torneos sin participantes
				s.mu.Unlock()
				time.Sleep(5 * time.Second)
				continue // Saltar al siguiente torneo
			}

			log.Printf("¡Comienzan los combates del torneo! (%d participantes)", participantes)

			//------------------------------------------------------------------------------------------------------
			// Usar timer para el período de combates
			combateTimer := time.NewTimer(5 * time.Second)
			<-combateTimer.C

			// Copia de los participantes para el torneo
			s.mu.Lock()
			participantesActivos := make([]*pb.Entrenador, len(torneo.Participantes))
			copy(participantesActivos, torneo.Participantes)
			s.mu.Unlock()

			// Mezclar a los participantes para emparejamientos aleatorios
			rand.Shuffle(len(participantesActivos), func(i, j int) {
				participantesActivos[i], participantesActivos[j] = participantesActivos[j], participantesActivos[i]
			})

			// Estructura de árbol de torneo
			// Si hay número impar de participantes, agregamos "byes" (pases automáticos)

			// Continuar con las rondas del torneo
			ronda := 1

			for len(participantesActivos) > 1 {
				log.Printf("Comenzando ronda %d con %d participantes", ronda, len(participantesActivos))
				siguienteRonda := make([]*pb.Entrenador, 0)

				// Procesar cada pareja de combatientes en esta ronda
				for i := 0; i < len(participantesActivos); i += 2 {
					// Si queda un participante sin pareja, pasa directamente a la siguiente ronda
					if i+1 >= len(participantesActivos) {
						siguienteRonda = append(siguienteRonda, participantesActivos[i])
						log.Printf("%s pasa a la siguiente ronda sin combatir", participantesActivos[i].Nombre)
						continue
					}

					ent1 := participantesActivos[i]
					ent2 := participantesActivos[i+1]
					log.Println("")
					log.Println("")
					log.Println("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
					log.Printf("Combate: %s vs %s", ent1.Nombre, ent2.Nombre)

					// Crear ID único para el combate
					combateID := fmt.Sprintf("%s-combate-%d-%d", torneo.TorneoID, ronda, i/2)

					// Declarar ganadorID variable
					var ganadorID string

					// Verificar primero si ya tenemos un resultado adelantado para este combate
					s.mu.Lock()
					resultadoAdelantado, existeResultado := s.resultadosAdelantados[combateID]
					if existeResultado {
						// Ya tenemos el resultado, no necesitamos esperar
						delete(s.resultadosAdelantados, combateID)
						s.mu.Unlock()
						log.Printf("Encontrado resultado adelantado para combate %s: ganador ID %s", combateID, resultadoAdelantado)
						ganadorID = resultadoAdelantado
					} else {
						// No tenemos el resultado todavía, crear canal para esperarlo
						combateResultado := make(chan string, 1)

						// Registrar este combate en un mapa global
						if s.combatesPendientes == nil {
							s.combatesPendientes = make(map[string]chan string)
						}
						s.combatesPendientes[combateID] = combateResultado
						s.mu.Unlock()

						// Intentar enviar solicitud de combate al servidor de gimnasios
						if gimnasiosClient == nil {
							// Si no hay conexión con gimnasios, elegir ganador aleatorio
							log.Printf("No hay conexión con gimnasios para combate %s, eligiendo ganador aleatorio", combateID)
							if rand.Intn(2) == 0 {
								ganadorID = ent1.Id
							} else {
								ganadorID = ent2.Id
							}
						} else {
							// Seleccionar un gimnasio aleatorio para este combate
							gimnasio := seleccionarGimnasio()
							log.Printf("Solicitando combate (%s) a central de gimnasios en %s (%s)", combateID, gimnasio.Nombre, gimnasio.Region)

							// Preparar la solicitud de combate
							solicitudCombate := &pb.SolicitudCombate{
								CombateId:          combateID,
								TorneoId:           torneo.TorneoID,
								Entrenador1Id:      ent1.Id,
								Entrenador2Id:      ent2.Id,
								GimnasioNombre:     gimnasio.Nombre,
								GimnasioRegion:     gimnasio.Region,
								Entrenador1Ranking: ent1.Ranking, // Añadir ranking real
								Entrenador2Ranking: ent2.Ranking, // Añadir ranking real
							}

							// Enviar la solicitud de combate
							ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

							resultadoCombate, err := gimnasiosClient.AsignarCombate(ctx, solicitudCombate)
							cancel() // Siempre hacer cancel después de la llamada, no usar defer aquí

							if err != nil {
								log.Printf("Error asignando combate: %v", err)
								// Si falla la asignación completamente, elegir ganador aleatorio
								if rand.Intn(2) == 0 {
									ganadorID = ent1.Id
								} else {
									ganadorID = ent2.Id
								}
							} else {
								// La solicitud fue exitosa pero ahora debemos esperar el resultado de CDP
								log.Printf("%s", resultadoCombate.Mensaje)

								// Esperar a que CDP nos envíe el resultado real a través del canal
								select {
								case ganadorID = <-combateResultado: // Añadir este caso para recibir el resultado

								case <-time.After(8 * time.Second): // Timeout después de 8 segundos
									log.Printf("Timeout esperando resultado desde CDP para combate %s, eligiendo ganador aleatorio", combateID)
									// Al no tener resultado de gimnasios, elegimos ganador aleatorio
									if rand.Intn(2) == 0 {
										ganadorID = ent1.Id
									} else {
										ganadorID = ent2.Id
									}
								}
							}
						}

						// Limpiar el canal del mapa
						s.mu.Lock()
						delete(s.combatesPendientes, combateID)
						s.mu.Unlock()
					}

					// Determinar ganador basado en el ID recibido
					if ganadorID == ent1.Id {
						siguienteRonda = append(siguienteRonda, ent1)
						log.Printf("Ganador oficial: %s", ent1.Nombre)
					} else {
						siguienteRonda = append(siguienteRonda, ent2)
						log.Printf("Ganador oficial: %s", ent2.Nombre)
					}
					log.Println("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
					log.Println("")
					log.Println("")
					// Pequeña pausa entre combates para visualización
					time.Sleep(500 * time.Millisecond)
				}

				// Pausa entre rondas para mejor visualización
				log.Println("")
				log.Println("+++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++")
				log.Printf("Fin de la ronda %d. Avanzando a la siguiente ronda con %d participantes", ronda-1, len(siguienteRonda))
				log.Println("+++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++++")
				log.Println("")
				time.Sleep(1 * time.Second)

				participantesActivos = siguienteRonda
				ronda++
			}

			// El último participante es el ganador del torneo
			if len(participantesActivos) == 1 {
				s.mu.Lock()
				torneo.Ganador = participantesActivos[0]

				// Aumentar el ranking del ganador (gana 100 puntos)
				if entGanador, exists := s.entrenadoresMap[torneo.Ganador.Id]; exists {
					entGanador.Ranking = entGanador.Ranking + 100
					log.Printf("¡%s gana 100 puntos de ranking por ganar el torneo! Nuevo ranking: %d",
						torneo.Ganador.Nombre, entGanador.Ranking)
				}

				log.Printf("¡El ganador del torneo %s (ID: %s) es %s!", torneo.Nombre, torneo.TorneoID, torneo.Ganador.Nombre)
				torneo.Estado = "finalizado"
				s.mu.Unlock()
			} else {
				log.Printf("Error: No se pudo determinar un ganador único para el torneo %s", torneo.Nombre)
			}

			log.Printf("¡El torneo %s (ID: %s) ha finalizado!", torneo.Nombre, torneo.TorneoID)
			log.Println("****************************************************************************************")
			// Esperar antes de iniciar otro torneo
			time.Sleep(5 * time.Second)
		}
	}()

	// Bloquear el programa para que no termine
	select {}
}

// Función para verificar si un mensaje de resultado de combate es válido y no duplicado
func (s *server) verificarMensajeCDP(in *pb.ResultadoCombateDesdeRabbit) error {
	// Verificar estructura básica del mensaje
	if in.CombateId == "" {
		return fmt.Errorf("mensaje inválido: ID de combate vacío")
	}

	if in.TorneoId == "" {
		return fmt.Errorf("mensaje inválido: ID de torneo vacío")
	}

	if in.Entrenador1Id == "" || in.Entrenador2Id == "" {
		return fmt.Errorf("mensaje inválido: IDs de entrenadores incompletos")
	}

	if in.GanadorId == "" {
		return fmt.Errorf("mensaje inválido: no se especificó un ganador")
	}

	// Verificar que el ID del ganador corresponda a uno de los entrenadores
	if in.GanadorId != in.Entrenador1Id && in.GanadorId != in.Entrenador2Id {
		return fmt.Errorf("mensaje inválido: el ID del ganador (%s) no corresponde a ninguno de los entrenadores del combate (%s, %s)",
			in.GanadorId, in.Entrenador1Id, in.Entrenador2Id)
	}

	// Verificar si es un mensaje duplicado
	s.ultimaProcesamiento.Lock()
	defer s.ultimaProcesamiento.Unlock()

	if s.combatesProcesados == nil {
		s.combatesProcesados = make(map[string]bool)
	}

	// Si el combate ya fue procesado, es un duplicado
	if s.combatesProcesados[in.CombateId] {
		return fmt.Errorf("mensaje duplicado: el resultado del combate %s ya fue procesado", in.CombateId)
	}

	// Verificar formato válido para la fecha
	if in.FechaCombate != "" {
		_, err := time.Parse("2006-01-02 15:04:05", in.FechaCombate)
		if err != nil {
			return fmt.Errorf("mensaje inválido: formato de fecha incorrecto: %v", err)
		}
	}

	return nil
}

func (s *server) VerificarEntrenadores(ctx context.Context, in *pb.VerificacionEntrenadores) (*pb.ResultadoVerificacion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	todosValidos := true

	for _, id := range in.EntrenadorIds {
		// Verificar si el entrenador existe y está activo
		entrenador, existe := s.entrenadoresMap[id]
		if !existe || entrenador.Estado != "activo" {
			todosValidos = false
			break
		}
	}

	resultado := &pb.ResultadoVerificacion{
		TodosValidos: todosValidos,
	}

	if todosValidos {
		resultado.Mensaje = "Todos los entrenadores están registrados y activos"
	} else {
		resultado.Mensaje = "Uno o ambos entrenadores no están registrados o no están activos"
	}

	log.Printf("Verificación de entrenadores solicitada para %v IDs, resultado: %v", len(in.EntrenadorIds), resultado.TodosValidos)
	return resultado, nil
}

// Function to connect to RabbitMQ with retries
func conectarRabbitMQ(url string, maxRetries int, retryDelay time.Duration) (*amqp.Connection, error) {
	var conn *amqp.Connection
	var err error

	for i := 0; i < maxRetries; i++ {
		conn, err = amqp.Dial(url)
		if err == nil {
			return conn, nil // Connection successful
		}

		log.Printf("Attempt %d: Could not connect to RabbitMQ: %v", i+1, err)
		time.Sleep(retryDelay) // Wait before retrying
	}

	return nil, fmt.Errorf("could not connect to RabbitMQ at %s after %d attempts", url, maxRetries)
}

// Function to process combat results from RabbitMQ
func (s *server) procesarResultadosCombates(ch *amqp.Channel, queueName string) {
	// Declare the queue to make sure it exists
	q, err := ch.QueueDeclare(
		queueName, // name
		false,     // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // no-wait
		nil,       // arguments
	)
	if err != nil {
		log.Printf("Failed to declare queue %s: %v", queueName, err)
		return
	}

	// Consume messages from the queue
	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		true,   // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	if err != nil {
		log.Printf("Failed to register a consumer for queue %s: %v", queueName, err)
		return
	}

	log.Printf("Started consuming combat results from RabbitMQ queue: %s", queueName)

	for d := range msgs {
		go s.handleCombatResult(d.Body)
	}
}

// Handle combat result from RabbitMQ
func (s *server) handleCombatResult(messageBody []byte) {
	// Parse the JSON message
	var resultado ResultadoCombateMQ
	if err := json.Unmarshal(messageBody, &resultado); err != nil {
		log.Printf("Error decodificando JSON del resultado del combate: %v", err)
		return
	}

	// Check for duplicates
	s.ultimaProcesamiento.Lock()
	if s.combatesProcesados == nil {
		s.combatesProcesados = make(map[string]bool)
	}

	// If the combat has already been processed, it's a duplicate
	if s.combatesProcesados[resultado.CombateID] {
		log.Printf("Mensaje duplicado: El resultado del combate %s ya fue procesado", resultado.CombateID)
		s.ultimaProcesamiento.Unlock()
		return
	}

	// Mark this combat as processed to avoid future duplicates
	s.combatesProcesados[resultado.CombateID] = true
	s.ultimaProcesamiento.Unlock()

	// Check if there's a channel waiting for this result
	s.mu.Lock()
	resultadoChan, esperando := s.combatesPendientes[resultado.CombateID]

	if esperando {
		// Si hay un canal esperando, enviar el resultado
		select {
		case resultadoChan <- resultado.GanadorID:
			log.Printf("Resultado del combate %s enviado al canal ", resultado.CombateID)
		default:
			log.Printf("No se pudo enviar resultado para combate %s (canal posiblemente cerrado)", resultado.CombateID)
		}
	} else {
		// Si no hay canal esperando, guardar como resultado adelantado
		if s.resultadosAdelantados == nil {
			s.resultadosAdelantados = make(map[string]string)
		}
		s.resultadosAdelantados[resultado.CombateID] = resultado.GanadorID
		log.Printf("Almacenando resultado del combate %s como resultado adelantado", resultado.CombateID)
	}
	s.mu.Unlock()

	// Look for the trainers involved
	s.mu.Lock()
	defer s.mu.Unlock()

	ent1, existe1 := s.entrenadoresMap[resultado.Entrenador1ID]
	ent2, existe2 := s.entrenadoresMap[resultado.Entrenador2ID]

	if !existe1 || !existe2 {
		log.Printf("Uno o ambos entrenadores no encontrados en la base de datos para el combate %s", resultado.CombateID)
		return
	}

	// Determine winner and loser based on the returned ID
	var ganador, perdedor *pb.Entrenador
	if resultado.GanadorID == ent1.Id {
		ganador = ent1
		perdedor = ent2
	} else {
		ganador = ent2
		perdedor = ent1
	}

	log.Printf("Resultado de %s vs %s en %s",
		ent1.Nombre, ent2.Nombre, resultado.GimnasioNombre)
	// Update loser's ranking (loses 50 points)
	if entPerdedor, exists := s.entrenadoresMap[perdedor.Id]; exists {
		entPerdedor.Ranking = entPerdedor.Ranking - 50
		if entPerdedor.Ranking < 0 {
			entPerdedor.Ranking = 0
		}
		log.Printf("CDP: %s pierde 50 puntos. Nuevo ranking: %d", perdedor.Nombre, entPerdedor.Ranking)
	}

	// Winner gains some ranking points
	if entGanador, exists := s.entrenadoresMap[ganador.Id]; exists {
		entGanador.Ranking = entGanador.Ranking + 25
		log.Printf("CDP: %s gana 25 puntos. Nuevo ranking: %d", ganador.Nombre, entGanador.Ranking)
	}

}
