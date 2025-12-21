package scrcpy

import (
	"fmt"
	"log"
	"webscreen/sdriver"
)

func (sd *ScrcpyDriver) GetReceivers() (<-chan sdriver.AVBox, <-chan sdriver.AVBox, <-chan sdriver.Event) {
	return sd.VideoChan, sd.AudioChan, sd.ControlChan
}

func (sd *ScrcpyDriver) Start() {
	log.Println("ScrcpyDriver: Start called")
	go sd.convertVideoFrame()
	go sd.convertAudioFrame()
	go sd.transferControlMsg()
}

func (sd *ScrcpyDriver) Pause() {
	// sd.stopVideoReader()
}

func (sd *ScrcpyDriver) SendEvent(event sdriver.Event) error {
	switch e := event.(type) {
	case *sdriver.TouchEvent:
		sd.SendTouchEvent(e)
	case *sdriver.KeyEvent:
		sd.SendKeyEvent(e)
	case *sdriver.ScrollEvent:
		sd.SendScrollEvent(e)
	case *sdriver.RotateEvent:
		sd.RotateDevice()
	case *sdriver.GetClipboardEvent:
		log.Printf("[ScrcpyDriver] SendEvent: GetClipboardEvent")
		sd.SendGetClipboardEvent(e)
	case *sdriver.SetClipboardEvent:
		sd.SendSetClipboardEvent(e)
	case *sdriver.UHIDCreateEvent:
		sd.SendUHIDCreateEvent(e)
	case *sdriver.UHIDInputEvent:
		sd.SendUHIDInputEvent(e)
	case *sdriver.UHIDDestroyEvent:
		sd.SendUHIDDestroyEvent(e)
	case *sdriver.ReqIDREvent:
		sd.KeyFrameRequest()
	default:
		// Fallback for IDR if passed as a different struct with same Type
		if event.Type() == sdriver.EVENT_TYPE_REQ_IDR {
			sd.KeyFrameRequest()
			return nil
		}
		log.Printf("ScrcpyDriver: Unhandled event type: %T", event)
	}

	return nil
}

func (sd *ScrcpyDriver) RequestIDR(firstFrame bool) {
	if len(sd.LastVPS) > 0 {
		select {
		case sd.VideoChan <- sdriver.AVBox{Data: createCopy(sd.LastVPS), PTS: sd.LastPTS, IsKeyFrame: false, IsConfig: true}:
		default:
		}
	}
	if len(sd.LastSPS) > 0 {
		select {
		case sd.VideoChan <- sdriver.AVBox{Data: createCopy(sd.LastSPS), PTS: sd.LastPTS, IsKeyFrame: false, IsConfig: true}:
		default:
		}
	}
	if len(sd.LastPPS) > 0 {
		select {
		case sd.VideoChan <- sdriver.AVBox{Data: createCopy(sd.LastPPS), PTS: sd.LastPTS, IsKeyFrame: false, IsConfig: true}:
		default:
		}
	}
	if len(sd.LastIDR) > 0 && firstFrame {
		select {
		case sd.VideoChan <- sdriver.AVBox{Data: createCopy(sd.LastIDR), PTS: sd.LastPTS, IsKeyFrame: true, IsConfig: false}:
		default:
		}
	}
	sd.KeyFrameRequest()
}

func (sd *ScrcpyDriver) Capabilities() sdriver.DriverCaps {
	return sd.capabilities
}

func (sd *ScrcpyDriver) MediaMeta() sdriver.MediaMeta {
	return sd.mediaMeta
}

func (sd *ScrcpyDriver) Stop() {
	if sd.videoConn != nil {
		sd.videoConn.Close()
	}
	if sd.audioConn != nil {
		sd.audioConn.Close()
	}
	if sd.controlConn != nil {
		sd.controlConn.Close()
	}
	sd.adbClient.ReverseRemove(fmt.Sprintf("localabstract:scrcpy_%s", sd.scid))
	sd.adbClient.Stop()
	sd.cancel()
}
