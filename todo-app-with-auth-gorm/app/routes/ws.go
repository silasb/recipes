package routes

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	// "numtostr/gotodo/app/services"
	// "numtostr/gotodo/utils/middleware"

	"context"

	"github.com/go-redis/redis/v8"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
)

var ctx = context.Background()

type client struct{} // Add more data to this type if needed

var clients = make(map[*websocket.Conn]client) // Note: although large maps with pointer-like types (e.g. strings) as keys are slow, using pointers themselves as keys is acceptable and fast
var register = make(chan *websocket.Conn)
var broadcast = make(chan string)
var unregister = make(chan *websocket.Conn)

// date	open	high	low	close	volume
const data = `
1460727000000	55.3	55.3	55.25	55.25	3399547
1460727060000	55.24	55.39	55.11	55.37	253961
1460727120000	55.37	55.5299	55.37	55.5299	127451
1460727180000	55.52	55.59	55.52	55.58	101263
1460727240000	55.58	55.59	55.5	55.51	123661
1460727300000	55.505	55.52	55.495	55.5065	149931
1460727360000	55.51	55.63	55.5	55.61	126985
1460727420000	55.605	55.64	55.52	55.55	86640
1460727480000	55.56	55.62	55.55	55.61	96088
1460727540000	55.61	55.62	55.56	55.585	62305
1460727600000	55.58	55.58	55.5	55.52	35996
1460727660000	55.52	55.55	55.5	55.5	39946
1460727720000	55.5	55.54	55.5	55.525	44578
1460727780000	55.52	55.53	55.5	55.5238	40415
1460727840000	55.52	55.53	55.51	55.5233	17257
1460727900000	55.525	55.535	55.5	55.52	22595
1460727960000	55.52	55.52	55.5	55.505	16175
`

type OHLCData struct {
	Date   string  `json:"date"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume int     `json:"volume"`
}

func redisStream() {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	// err := rdb.Set(ctx, "key", "value", 0).Err()
	// if err != nil {
	//     panic(err)
	// }

	// val, err := rdb.Get(ctx, "key").Result()
	// if err != nil {
	//     panic(err)
	// }
	// fmt.Println("key", val)
	x, err := rdb.XReadStreams(ctx, "test").Result()
	if err != nil {
		panic(err)
	}

	fmt.Println(x)
}

func runHub() {
	reader := strings.NewReader(data)
	tsvreader := csv.NewReader(reader)

	tsvreader.Comma = '\t' // Use tab-delimited instead of comma <---- here!

	tsvreader.FieldsPerRecord = -1

	tsvData, err := tsvreader.ReadAll()
	if err != nil {
		panic(err)
		// fmt.Println(err)
		// os.Exit(1)
	}

	var oneRecord OHLCData
	var allRecords []OHLCData

	for _, each := range tsvData {
		oneRecord.Date = each[0]
		oneRecord.Open, _ = strconv.ParseFloat(each[1], 64)
		oneRecord.High, _ = strconv.ParseFloat(each[2], 64)
		oneRecord.Low, _ = strconv.ParseFloat(each[3], 64)
		oneRecord.Close, _ = strconv.ParseFloat(each[4], 64)
		oneRecord.Volume, _ = strconv.Atoi(each[5])
		// oneRecord.Job = each[2]
		allRecords = append(allRecords, oneRecord)
	}

	counter := 0

	for {
		select {
		case connection := <-register:
			clients[connection] = client{}
			log.Println("connection registered")

		case message := <-broadcast:
			log.Println("message received:", message)

			// Send the message to all clients
			for connection := range clients {
				if err := connection.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
					log.Println("write error:", err)

					unregister <- connection
					connection.WriteMessage(websocket.CloseMessage, []byte{})
					connection.Close()
				}
			}

		case connection := <-unregister:
			// Remove the client from the hub
			delete(clients, connection)

			log.Println("connection unregistered")
		case <-time.After(1 * time.Second):
			counter = counter + 1
			jsondata, _ := json.Marshal(allRecords[counter%len(allRecords)])

			for connection := range clients {
				connection.WriteMessage(websocket.TextMessage, jsondata)
			}
		}

	}
}

// TodoRoutes contains all routes relative to /todo
func WS(app fiber.Router) {
	app.Use(func(c *fiber.Ctx) error {
		if websocket.IsWebSocketUpgrade(c) { // Returns true if the client requested upgrade to the WebSocket protocol
			return c.Next()
		}
		return c.SendStatus(fiber.StatusUpgradeRequired)
	})

	app.Get("/ws", websocket.New(func(c *websocket.Conn) {
		// When the function returns, unregister the client and close the connection
		defer func() {
			unregister <- c
			c.Close()
		}()

		// Register the client
		register <- c

		for {
			messageType, message, err := c.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Println("read error:", err)
				}

				return // Calls the deferred function, i.e. closes the connection on error
			}

			if messageType == websocket.TextMessage {
				// Broadcast the received message
				log.Println(message)
				broadcast <- string(message)
			} else {
				log.Println("websocket message received of type", messageType)
			}
		}
	}))

	go runHub()
}
