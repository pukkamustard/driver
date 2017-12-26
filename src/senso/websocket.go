package senso

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/grandcat/zeroconf"
	"github.com/sirupsen/logrus"
)

// WEBSOCKET PROTOCOL

// Command sent by Play
type Command struct {
	*GetStatus

	*Connect
	*Disconnect

	*Discover
}

func prettyPrintCommand(command Command) string {
	if command.GetStatus != nil {
		return "GetStatus"
	} else if command.Connect != nil {
		return "Connect"
	} else if command.Disconnect != nil {
		return "Disconnect"
	} else if command.Disconnect != nil {
		return "Discover"
	}
	return "Unknown"
}

// GetStatus command
type GetStatus struct{}

// Connect command
type Connect struct {
	Address string `json:"address"`
}

// Disconnect command
type Disconnect struct{}

// Discover command
type Discover struct {
	Duration int `json:"duration"`
}

// UnmarshalJSON implements encoding/json Unmarshaler interface
func (command *Command) UnmarshalJSON(data []byte) error {

	// Helper struct to get type
	temp := struct {
		Type string `json:"type"`
	}{}
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	if temp.Type == "GetStatus" {
		command.GetStatus = &GetStatus{}

	} else if temp.Type == "Connect" {
		err := json.Unmarshal(data, &command.Connect)
		if err != nil {
			return err
		}

	} else if temp.Type == "Disconnect" {
		command.Disconnect = &Disconnect{}

	} else if temp.Type == "Discover" {

		err := json.Unmarshal(data, &command.Discover)
		if err != nil {
			return err
		}

	} else {
		return errors.New("can not decode unknown command")
	}

	return nil
}

// Message that can be sent to Play
type Message struct {
	*Status

	Discovered *zeroconf.ServiceEntry
}

// Status is a message containing status information
type Status struct {
	Address *string
}

// MarshalJSON ipmlements JSON encoder for messages
func (message *Message) MarshalJSON() ([]byte, error) {
	if message.Status != nil {
		return json.Marshal(&struct {
			Type    string  `json:"type"`
			Address *string `json:"address"`
		}{
			Type:    "Status",
			Address: message.Status.Address,
		})

	} else if message.Discovered != nil {
		return json.Marshal(&struct {
			Type         string                 `json:"type"`
			ServiceEntry *zeroconf.ServiceEntry `json:"service"`
			IP           []net.IP               `json:"ip"`
		}{
			Type:         "Discovered",
			ServiceEntry: message.Discovered,
			IP:           append(message.Discovered.AddrIPv4, message.Discovered.AddrIPv6...),
		})

	}

	return nil, errors.New("could not marshal message")

}

// Implement net/http Handler interface
func (handle *Handle) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	// Set up logger
	var log = handle.log.WithFields(logrus.Fields{
		"clientAddress": r.RemoteAddr,
		"userAgent":     r.UserAgent(),
	})

	// Update to WebSocket
	conn, err := webSocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.WithError(err).Error("Could not upgrade connection to WebSocket.")
		http.Error(w, "WebSocket upgrade error", http.StatusBadRequest)
		return
	}

	log.Info("WebSocket connection opened")

	// create a mutex for writing to WebSocket (connection supports only one concurrent reader and one concurrent writer (https://godoc.org/github.com/gorilla/websocket#hdr-Concurrency))
	writeMutex := sync.Mutex{}

	// Send data up the WebSocket
	send := func(data []byte) error {
		writeMutex.Lock()
		conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
		err := conn.WriteMessage(websocket.BinaryMessage, data)
		writeMutex.Unlock()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.WithError(err).Error("WebSocket error")
			}
			return err
		}
		return nil
	}

	// Create channels with data received from Senso
	rx := handle.broker.Sub("rx")

	// send data from Control and Data channel
	go func() {
		var err error
		for {
			select {
			case i := <-rx:
				data, ok := i.([]byte)
				if ok {
					err = send(data)
				}
			}

			if err != nil {
				return
			}
		}
	}()

	close := func() {
		// Unsubscribe from broker
		handle.broker.Unsub(rx)

		// Close websocket connection
		conn.Close()

		log.Info("Websocket connection closed")
	}
	// receive messages
	go func() {
		defer close()
		for {

			messageType, msg, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					log.WithError(err).Error("WebSocket error")
				}
				return
			}

			if messageType == websocket.BinaryMessage {
				handle.broker.TryPub(msg, "tx")

			} else if messageType == websocket.TextMessage {

				var command Command
				decodeErr := json.Unmarshal(msg, &command)
				if decodeErr != nil {
					log.WithField("rawCommand", msg).WithError(decodeErr).Warning("Can not decode command.")
					continue
				}

				log.WithField("command", prettyPrintCommand(command)).Debug("Received command.")

				if command.GetStatus != nil {

					var message Message

					message.Status = &Status{Address: handle.Address}

					writeMutex.Lock()
					conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
					writeErr := conn.WriteJSON(&message)
					writeMutex.Unlock()

					if writeErr != nil {
						log.WithError(writeErr).Error("WebSocket write error.")
						continue
					}

				} else if command.Connect != nil {
					handle.Connect(command.Connect.Address)

				} else if command.Disconnect != nil {
					handle.Disconnect()

				} else if command.Discover != nil {

					discoveryCtx, cancelDiscovery := context.WithTimeout(context.Background(), time.Duration(command.Discover.Duration)*time.Second)
					defer cancelDiscovery()

					entries := handle.Discover(discoveryCtx)

					go func(entries chan *zeroconf.ServiceEntry) {
						for entry := range entries {
							log.WithField("service", entry).Debug("Discovered service.")

							var message Message
							message.Discovered = entry

							writeMutex.Lock()
							conn.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
							writeErr := conn.WriteJSON(&message)
							writeMutex.Unlock()

							if writeErr != nil {
								log.WithError(writeErr).Error("WebSocket write error.")
							}

						}
						log.Debug("Discovery finished.")
					}(entries)

				}

			}

		}
	}()

}

// HELPERS

// Helper to upgrade http to WebSocket
var webSocketUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}
