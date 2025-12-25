package sagent

import (
	"log"
	"webscreen/sdriver"
)

func (sa *Agent) EventFeedback(handler func([]byte) bool) {
	for event := range sa.controlCh {
		// log.Printf("[Agent] Received event: %+v", event)
		eType := event.Type()
		switch eType {
		case sdriver.EVENT_TYPE_RECEIVE_CLIPBOARD:
			event := event.(sdriver.SampleEvent)
			content := event.GetContent()
			msg := make([]byte, 1+len(content))
			copy(msg[1:], content)
			msg[0] = byte(sdriver.EVENT_TYPE_RECEIVE_CLIPBOARD)
			if !handler(msg) {
				return
			}
		case sdriver.EVENT_TYPE_TEXT_MSG:
			event := event.(sdriver.TextMsgEvent)
			content := []byte(event.Msg)
			msg := make([]byte, 1+len(content))
			copy(msg[1:], content)
			msg[0] = byte(sdriver.EVENT_TYPE_TEXT_MSG)
			if !handler(msg) {
				return
			}
		default:
			log.Printf("Unhandled event type in ReceiveEvent: %d", eType)
		}
	}
}
