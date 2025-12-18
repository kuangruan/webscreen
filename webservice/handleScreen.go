package webservice

import (
	"log"
	"net/http"
	sagent "webcpy/streamAgent"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func handleScreen(c *gin.Context) {
	http.ServeFile(c.Writer, c.Request, "./public/screen.html")
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for development
	},
}

// /:device_type/:device_id/:device_ip/:device_port/ws
func (wm *WebMaster) handleScreenWS(c *gin.Context) {
	// Implement WebSocket handling for screen here
	// Parse URL parameters
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Println("Failed to upgrade to websocket:", err)
		return
	}
	deviceType := c.Param("device_type")
	deviceID := c.Param("device_id")
	deviceIP := c.Param("device_ip")
	devicePort := c.Param("device_port")
	config := sagent.ConnectionConfig{}
	err = conn.ReadJSON(&config)
	if err != nil {
		log.Println("Failed to read connection options:", err)
		conn.Close()
		return
	}
	if config.DeviceType != deviceType || config.DeviceID != deviceID || config.DeviceIP != deviceIP || config.DevicePort != devicePort {
		log.Println("Connection options do not match URL parameters")
		conn.Close()
		return
	}
	// Create a unique session ID
	sessionID := config.DeviceType + "_" + config.DeviceID + "_" + config.DeviceIP + "_" + config.DevicePort
	log.Printf("New WebSocket connection for session: %s", sessionID)
	session := wm.ScreenSessions[sessionID]
	session.WSConn = conn

	agent, err := sagent.NewAgent(config)
	if err != nil {
		log.Println("Failed to create agent:", err)
		conn.Close()
		return
	}
	session.Agent = agent
	wm.ScreenSessions[sessionID] = session

	finalSDP := agent.CreateWebRTCConnection(string(config.SDP))
	// conn.WriteMessage(websocket.TextMessage, []byte(finalSDP))
	capabilities := agent.Capabilities()
	log.Printf("Driver Capabilities: %+v", capabilities)
	conn.WriteJSON(map[string]interface{}{"capabilities": capabilities, "sdp": finalSDP})
	go listenScreenWS(conn, agent)

	agent.StartStreaming()
}

func listenScreenWS(wsConn *websocket.Conn, agent *sagent.Agent) {
	for {
		mType, msg, err := wsConn.ReadMessage()
		if err != nil {
			log.Println("WebSocket read error:", err)
			break
		}

		switch mType {
		case websocket.BinaryMessage:
			log.Printf("Received binary message: %s", string(msg))
			agent.SendControlEvent(msg)
		case websocket.TextMessage:
			log.Printf("Received text message: %s", string(msg))
		default:
			log.Printf("Received unsupported message type: %d", mType)
		}
	}
	wsConn.Close()
	// Implement a listener for screen WebSocket connections if needed
	// for {
	// 	// Read message from client
	// 	messageType, p, err := conn.ReadMessage()
	// 	// log.Println("receive ws message type: ", messageType)
	// 	if err != nil {
	// 		log.Println("WebSocket read error:", err)
	// 		break
	// 	}
	// 	switch messageType {
	// 	case websocket.BinaryMessage:
	// 		// 处理二进制消息 (控制命令)
	// 		// log.Println("msg type:", p[0])
	// 		switch p[0] {
	// 		case WS_TYPE_TOUCH: // Touch Event
	// 			event, err := sm.createScrcpyTouchEvent(p)
	// 			if err != nil {
	// 				log.Println("Failed to unmarshal touch event:", err)
	// 				continue
	// 			}
	// 			// log.Printf("Touch Event: %+v\n", event)
	// 			sm.DataAdapter.SendTouchEvent(event)

	// 		case WS_TYPE_KEY: // Key Event
	// 			event, err := sm.createScrcpyKeyEvent(p)
	// 			// log.Printf("Key Event: %+v\n", event)
	// 			if err != nil {
	// 				log.Println("Failed to unmarshal key event:", err)
	// 				continue
	// 			}
	// 			sm.DataAdapter.SendKeyEvent(event)
	// 			// log.Println("key event sent")
	// 		case WS_TYPE_ROTATE: // Rotate Device
	// 			// log.Println("Rotate Device command received")
	// 			sm.DataAdapter.RotateDevice()
	// 		case WS_TYPE_SCROLL: // Scroll Event
	// 			event, err := sm.createScrcpyScrollEvent(p)
	// 			// log.Printf("Scroll Event: %+v\n", event)
	// 			if err != nil {
	// 				log.Println("Failed to unmarshal scroll event:", err)
	// 				continue
	// 			}
	// 			sm.DataAdapter.SendScrollEvent(event)
	// 		case WS_TYPE_UHID_CREATE: // UHID Create
	// 			event, err := sm.createScrcpyUHIDCreateEvent(p)
	// 			if err != nil {
	// 				log.Println("Failed to unmarshal uhid create event:", err)
	// 				continue
	// 			}
	// 			// log.Printf("UHID Create Event: %+v\n", event)
	// 			sm.DataAdapter.SendUHIDCreateEvent(event)
	// 			// log.Fatalln("UHID Create Event sent, exiting for debug")
	// 		case WS_TYPE_UHID_INPUT: // UHID Input
	// 			event, err := sm.createScrcpyUHIDInputEvent(p)
	// 			if err != nil {
	// 				log.Println("Failed to unmarshal uhid input event:", err)
	// 				continue
	// 			}
	// 			// log.Printf("UHID Input Event: %+v\n", event)
	// 			sm.DataAdapter.SendUHIDInputEvent(event)
	// 		case WS_TYPE_UHID_DESTROY: // UHID Destroy
	// 			event, err := sm.createScrcpyUHIDDestroyEvent(p)
	// 			if err != nil {
	// 				log.Println("Failed to unmarshal uhid destroy event:", err)
	// 				continue
	// 			}
	// 			// log.Printf("UHID Destroy Event: %+v\n", event)
	// 			sm.DataAdapter.SendUHIDDestroyEvent(event)
	// 		case WS_TYPE_SET_CLIPBOARD:
	// 			if len(p) > 1 {
	// 				content := string(p[1:])
	// 				sm.DataAdapter.SendSetClipboardEvent(content, true)
	// 			}
	// 		case WS_TYPE_GET_CLIPBOARD:
	// 			sm.DataAdapter.SendGetClipboardEvent()
	// 		default:
	// 			log.Println("Unknown control message type:", p[0])
	// 		}
	// 	default:
	// 		log.Println("Unsupported WebSocket message type:", messageType)
	// 	}

	// }

}
