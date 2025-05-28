package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "entrenador/proto/grpc-server/proto"

	"google.golang.org/grpc"
)

// Estructura para registrar actualizaciones de ranking
type ActualizacionRanking struct {
	EntrenadorID     string    `json:"entrenador_id"`
	EntrenadorNombre string    `json:"entrenador_nombre"`
	RankingAnterior  int32     `json:"ranking_anterior"`
	RankingNuevo     int32     `json:"ranking_nuevo"`
	Diferencia       int32     `json:"diferencia"`
	Timestamp        time.Time `json:"timestamp"`
}

// Función para cargar entrenadores desde el archivo JSON
func cargarEntrenadoresDesdeJSON(nombreArchivo string) ([]*pb.Entrenador, error) {
	file, err := os.ReadFile(nombreArchivo)
	if err != nil {
		return nil, err
	}

	var entrenadores []*pb.Entrenador
	if err := json.Unmarshal(file, &entrenadores); err != nil {
		return nil, err
	}

	return entrenadores, nil
}

// Función para inscribir automáticamente los entrenadores del archivo JSON
func inscribirEntrenadoresAutomaticamente(client pb.LCPServiceClient, entrenadores []*pb.Entrenador,
	entYaInscritos *sync.Map) {

	// Mapa para registrar los torneos en los que ya se intentó inscribir a todos los entrenadores
	var torneosIntentados sync.Map

	go func() {
		for {
			// Verificar si hay un torneo activo en fase de inscripción
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			torneos, err := client.ObtenerTorneos(ctx, &pb.Empty{})
			cancel()

			if err != nil {
				log.Printf("Error obteniendo torneos para inscripción automática: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}

			// Buscar un torneo en fase de inscripción
			var torneoActivo *pb.InfoTorneo
			for _, t := range torneos.Torneos {
				if t.Estado == "inscripcion" {
					torneoActivo = t
					break
				}
			}

			// Si no hay torneo activo, esperar y reintentar
			if torneoActivo == nil {
				time.Sleep(5 * time.Second)
				continue
			}

			// Verificar si ya se intentó inscribir en este torneo
			if _, yaIntentado := torneosIntentados.Load(torneoActivo.TorneoId); yaIntentado {
				// Ya se intentó inscribir a todos los entrenadores en este torneo
				time.Sleep(5 * time.Second)
				continue
			}

			// Intentar inscribir a cada entrenador una vez para este torneo
			for i, e := range entrenadores {
				// Verificar si ya se inscribió a este entrenador en este torneo
				key := e.Id + "-" + torneoActivo.TorneoId
				if _, inscrito := entYaInscritos.Load(key); inscrito {
					continue
				}

				// Si el estado es "Inactivo", nunca intentar inscribir
				if strings.EqualFold(e.Estado, "Inactivo") {
					//log.Printf("Entrenador %s está inactivo y no puede ser inscrito", e.Nombre)
					continue
				}

				// Procesar suspensión si existe
				if e.Suspension > 0 {
					// Reducir suspensión en 1
					entrenadores[i].Suspension--
					//log.Printf("Entrenador %s tiene suspensión. Reducido a %d torneo(s)",
					//	e.Nombre, entrenadores[i].Suspension)

					// Si la suspensión llegó a 0, restaurar estado a "Activo"
					if entrenadores[i].Suspension == 0 {
						entrenadores[i].Estado = "Activo"
						//	log.Printf("Suspensión de %s ha terminado. Estado cambiado a Activo", e.Nombre)
					}
					continue // No intentar inscribir mientras esté suspendido
				}

				// Si llegamos aquí, el entrenador está activo y sin suspensión
				// Preparar solicitud de inscripción
				solicitud := &pb.SolicitudInscripcion{
					EntrenadorId:      e.Id,
					EntrenadorNombre:  e.Nombre,
					EntrenadorRegion:  e.Region,
					EntrenadorRanking: e.Ranking,
				}

				// Enviar solicitud
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				resultado, err := client.InscribirEntrenador(ctx, solicitud)
				cancel()

				if err != nil {
					log.Printf("Error inscribiendo a %s: %v", e.Nombre, err)
					continue
				}

				if resultado.Exito {

					entYaInscritos.Store(key, true)
				} else {
					fmt.Printf(" No se pudo inscribir a %s en torneo %s: %s",
						e.Nombre, torneoActivo.TorneoId, resultado.Mensaje)
				}
			}

			// Marcar este torneo como ya intentado
			torneosIntentados.Store(torneoActivo.TorneoId, true)
			// Esperar antes del siguiente intento
			time.Sleep(5 * time.Second)
		}
	}()
}

// Función para generar un ID único basado en el nombre y un número aleatorio
func generarID(nombre string) string {
	// Tomar las primeras letras del nombre
	nombreBase := strings.ToLower(nombre)
	if len(nombreBase) > 5 {
		nombreBase = nombreBase[:5]
	}

	// Agregar un número aleatorio para hacer el ID único
	rand.Seed(time.Now().UnixNano())
	randomNum := rand.Intn(1000)

	return fmt.Sprintf("%s%d", nombreBase, randomNum)
}

// Función para leer input del usuario
func leerInput(prompt string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(prompt)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
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

func mostrarMenu() {
	fmt.Println("\n=== Menú Principal =============================")
	fmt.Println("1. Consultar torneo activo (gRPC)")
	fmt.Println("2. Inscribirse en torneo")
	fmt.Println("3. Ver estado actual")
	fmt.Println("4. Ver historial de global") // Nueva opción
	fmt.Println("5. Salir")
	fmt.Print("Seleccione una opción: ")

}

// Función para mostrar el historial de rankings
func mostrarHistorialRankings(archivoJSON string) {
	// Leer el archivo JSON
	contenido, err := os.ReadFile(archivoJSON)
	if err != nil {
		log.Printf("Error al leer el archivo de historial: %v\n", err)
		return
	}

	// Deserializar los datos
	var actualizaciones []ActualizacionRanking
	if err := json.Unmarshal(contenido, &actualizaciones); err != nil {
		log.Printf("Error al analizar el archivo de historial: %v\n", err)
		return
	}

	// Verificar si hay datos
	if len(actualizaciones) == 0 {
		fmt.Println("\n=== Historial de Rankings ===")
		fmt.Println("No hay cambios de ranking registrados todavía.")
		fmt.Println("=============================")
		return
	}

	// Mostrar los datos en formato de tabla
	fmt.Println("\n=== Historial de Rankings ===")
	fmt.Printf("%-20s %-10s %-10s %-10s %-10s\n",
		"Entrenador", "Anterior", "Nuevo", "Cambio", "Fecha")
	fmt.Println(strings.Repeat("-", 70))

	// Mostrar cada actualización
	for _, a := range actualizaciones {
		fmt.Printf("%-20s %-10d %-10d %-10d %s\n",
			a.EntrenadorNombre,
			a.RankingAnterior,
			a.RankingNuevo,
			a.Diferencia,
			a.Timestamp.Format("02/01 15:04:05"))
	}
	fmt.Println(strings.Repeat("-", 70))
	fmt.Printf("Total: %d registros de cambios\n", len(actualizaciones))
	fmt.Println("=============================")
}

// Función para monitorear y registrar cambios en rankings de entrenadores
func monitorearYGuardarRankings(cliente pb.LCPServiceClient, entrenadoresLocales []*pb.Entrenador, entrenadorID string, rankingPtr *int) {

	// Mapa para almacenar el último ranking conocido de cada entrenador
	ultimosRankings := make(map[string]int32)

	// Nombre del archivo JSON donde se guardarán los registros
	const archivoJSON = "historial_rankings.json"

	// Asegurar que podemos escribir en el archivo
	if err := verificarArchivoJSON(archivoJSON); err != nil {
		log.Printf("Error al preparar archivo de historial: %v", err)
		return
	}

	// Goroutine para monitoreo continuo
	go func() {

		for {
			// Esperar 5 segundos entre cada consulta
			time.Sleep(5 * time.Second)

			// Obtener lista actual de entrenadores
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			listaActual, err := cliente.ObtenerListaEntrenadores(ctx, &pb.Empty{})
			cancel()

			if err != nil {
				log.Printf("Error obteniendo lista de entrenadores para monitoreo: %v", err)
				continue
			}

			// Lista para almacenar actualizaciones de esta consulta
			var actualizacionesNuevas []ActualizacionRanking

			// Verificar cambios para cada entrenador
			for _, entrenadorActualizado := range listaActual.Entrenadores {
				// Si es la primera vez que vemos a este entrenador, solo guardamos su ranking
				if _, existe := ultimosRankings[entrenadorActualizado.Id]; !existe {
					ultimosRankings[entrenadorActualizado.Id] = entrenadorActualizado.Ranking
					continue
				}

				// Si hubo cambio en el ranking
				if ultimosRankings[entrenadorActualizado.Id] != entrenadorActualizado.Ranking {
					rankingAnterior := ultimosRankings[entrenadorActualizado.Id]
					rankingNuevo := entrenadorActualizado.Ranking
					diferencia := rankingNuevo - rankingAnterior

					// Crear registro de la actualización
					actualizacion := ActualizacionRanking{
						EntrenadorID:     entrenadorActualizado.Id,
						EntrenadorNombre: entrenadorActualizado.Nombre,
						RankingAnterior:  rankingAnterior,
						RankingNuevo:     rankingNuevo,
						Diferencia:       diferencia,
						Timestamp:        time.Now(),
					}

					// Añadir a la lista de actualizaciones nuevas
					actualizacionesNuevas = append(actualizacionesNuevas, actualizacion)

					// Actualizar el ranking en el mapa
					ultimosRankings[entrenadorActualizado.Id] = entrenadorActualizado.Ranking

					// Actualizar el ranking en la lista local de entrenadores
					for i, entrenadorLocal := range entrenadoresLocales {
						if entrenadorLocal.Id == entrenadorActualizado.Id {
							entrenadoresLocales[i].Ranking = entrenadorActualizado.Ranking
							break
						}
					}

					// Actualizar la variable de ranking del usuario actual si corresponde
					if entrenadorActualizado.Id == entrenadorID {
						*rankingPtr = int(rankingNuevo)
					}

				}
			}

			// Si hay actualizaciones, guardarlas en el archivo
			if len(actualizacionesNuevas) > 0 {
				if err := guardarActualizacionesRanking(archivoJSON, actualizacionesNuevas); err != nil {
					log.Printf("Error al guardar actualizaciones: %v", err)
				}
			}
		}
	}()
}

// Función para verificar/crear el archivo JSON
func verificarArchivoJSON(archivoJSON string) error {
	// Verificar si el archivo existe
	if _, err := os.Stat(archivoJSON); os.IsNotExist(err) {
		// El archivo no existe, crearlo con un array vacío
		contenidoInicial := []byte("[]")
		if err := os.WriteFile(archivoJSON, contenidoInicial, 0644); err != nil {
			return fmt.Errorf("error creando archivo inicial: %v", err)
		}
	}
	return nil
}

// Función para guardar actualizaciones en el archivo JSON
func guardarActualizacionesRanking(archivoJSON string, nuevasActualizaciones []ActualizacionRanking) error {
	// Leer archivo existente
	contenidoExistente, err := os.ReadFile(archivoJSON)
	if err != nil {
		return fmt.Errorf("error leyendo archivo existente: %v", err)
	}

	// Deserializar el contenido actual
	var actualizacionesExistentes []ActualizacionRanking
	if err := json.Unmarshal(contenidoExistente, &actualizacionesExistentes); err != nil {
		// Si hay error al deserializar, puede ser que el archivo esté corrupto o vacío
		actualizacionesExistentes = []ActualizacionRanking{} // Reiniciar con array vacío
	}

	// Añadir las nuevas actualizaciones
	actualizacionesActualizadas := append(actualizacionesExistentes, nuevasActualizaciones...)

	// Serializar a JSON con formato bonito
	contenidoNuevo, err := json.MarshalIndent(actualizacionesActualizadas, "", "  ")
	if err != nil {
		return fmt.Errorf("error serializando actualizaciones: %v", err)
	}

	// Escribir al archivo
	if err := os.WriteFile(archivoJSON, contenidoNuevo, 0644); err != nil {
		return fmt.Errorf("error escribiendo actualizaciones: %v", err)
	}

	return nil
}

func main() {
	const (
		lcpAddress = "localhost:50051"
	)

	// Cargar entrenadores del archivo JSON
	entrenadores, err := cargarEntrenadoresDesdeJSON("entrenadores_pequeno.json")
	if err != nil {
		log.Printf("Error al cargar entrenadores desde archivo: %v", err)
		// Continuamos con la aplicación normal aunque falle la carga
	}

	// Mapa para llevar registro de entrenadores ya inscritos
	var entYaInscritos sync.Map

	fmt.Println("¡Bienvenido al sistema de torneos Pokémon!")
	fmt.Println("=== Personaliza tu entrenador ===")

	// Solicitar información del entrenador
	nombre := leerInput("Nombre de entrenador: ")
	region := leerInput("Región de origen: ")
	rankingStr := leerInput("Ranking actual: ")

	// Convertir ranking a entero
	ranking, err := strconv.Atoi(rankingStr)
	if err != nil || ranking < 1 || ranking > 10000 {
		fmt.Println("Ranking inválido. Usando valor predeterminado: 1500")
		ranking = 1500
	}

	// Generar un ID único basado en el nombre
	entrenadorID := generarID(nombre)

	fmt.Printf("\n¡Bienvenido, %s de %s (Ranking: %d)!\n", nombre, region, ranking)
	fmt.Printf("Tu ID de entrenador es: %s\n", entrenadorID)
	fmt.Println("Conectando al servidor de torneos...")

	// Base context for the application
	baseCtx := context.Background()

	// Conexión al servicio LCP
	connLCP, err := conectarGRPC(lcpAddress, 5, 2*time.Second)
	if err != nil {
		log.Fatalf("No se pudo conectar al servicio LCP: %v", err)
	}
	defer connLCP.Close()

	// Crear cliente para el servicio LCP
	lcpClient := pb.NewLCPServiceClient(connLCP)

	// Iniciar monitoreo automático de rankings
	monitorearYGuardarRankings(lcpClient, entrenadores, entrenadorID, &ranking)

	// Iniciar la goroutine para inscripción automática si se cargaron entrenadores
	if len(entrenadores) > 0 {
		inscribirEntrenadoresAutomaticamente(lcpClient, entrenadores, &entYaInscritos)
	}

	// Menú interactivo con mejor lectura de input
	reader := bufio.NewReader(os.Stdin)
	for {
		mostrarMenu()
		opcion, _ := reader.ReadString('\n')
		opcion = strings.TrimSpace(opcion)
		fmt.Println("====================================================")

		switch opcion {
		case "1":
			fmt.Println("Consultando torneo activo...")

			// Crear contexto con timeout para esta operación
			opCtx, opCancel := context.WithTimeout(baseCtx, 5*time.Second)
			torneos, err := lcpClient.ObtenerTorneos(opCtx, &pb.Empty{})
			opCancel()

			if err != nil {
				log.Printf("Error al obtener torneos: %v", err)
				continue
			}

			if len(torneos.Torneos) == 0 {
				fmt.Println("No hay torneos registrados en este momento.")
			} else {
				//imprimirTorneos(torneos)

				// Buscar e imprimir información específica del torneo activo
				var torneoActivo *pb.InfoTorneo
				for _, t := range torneos.Torneos {
					if t.Estado == "inscripcion" {
						torneoActivo = t
						fmt.Println("\n=== Torneo Activo ===")
						fmt.Printf("Nombre: %s\nEstado: %s\n", t.Nombre, t.Estado)
						fmt.Printf("Tu puedes participar como: %s de %s (ID: %s)\n", nombre, region, entrenadorID)
						fmt.Println("¡Inscripciones abiertas! Use la opción 2 para inscribirse")
						break
					}
				}

				if torneoActivo == nil {
					fmt.Println("\nNo hay torneos con inscripciones abiertas en este momento.")
				}
			}

		case "2":
			fmt.Println("Inscribiéndose en torneo...")

			// Crear una solicitud de inscripción con todos los datos del entrenador
			solicitud := &pb.SolicitudInscripcion{
				EntrenadorId:      entrenadorID,
				EntrenadorNombre:  nombre,
				EntrenadorRegion:  region,
				EntrenadorRanking: int32(ranking),
			}

			// Crear contexto con timeout para esta operación
			opCtx, opCancel := context.WithTimeout(baseCtx, 5*time.Second)
			resultado, err := lcpClient.InscribirEntrenador(opCtx, solicitud)
			opCancel()

			if err != nil {
				fmt.Printf("Error al intentar inscribirse: %v", err)
				continue
			}

			// Mostrar resultado de la inscripción
			if resultado.Exito {
				fmt.Printf("O - ¡%s de %s se ha inscrito con éxito!\n", nombre, region)
				fmt.Printf("   %s\n", resultado.Mensaje)
			} else {
				fmt.Printf("X - No se pudo inscribir: %s\n", resultado.Mensaje)
			}

		case "3":
			fmt.Println("Mostrando estado actual...")
			if err != nil {
				fmt.Printf("Error al obtener lista de entrenadores: %v", err)
				continue
			}

			fmt.Println("\n=== Tu  ======")
			fmt.Printf("Nombre: %s\n", nombre)
			fmt.Printf("Región: %s\n", region)
			fmt.Printf("Ranking: %d\n", ranking)
			fmt.Printf("ID: %s\n", entrenadorID)
			fmt.Println("================")

		case "4":
			mostrarHistorialRankings("historial_rankings.json")

		case "5": // Cambiar el número de la opción de salida
			fmt.Println("Saliendo...")
			return

		default:
			fmt.Println("Opción no válida. Por favor, intente nuevamente.")
		}
	}
}
