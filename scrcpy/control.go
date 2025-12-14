package scrcpy

import (
	"encoding/binary"
	"log"
	"time"
)

func (da *DataAdapter) SendTouchEvent(e TouchEvent) {
	if da.controlConn == nil {
		return
	}
	// log.Printf("sending touch event: %v\n", e)
	// 1. 预分配一个固定大小的字节切片 (Scrcpy 协议触摸包固定 28 字节)
	// 这里的 buf 可以在对象池(sync.Pool)里复用，进一步减少 GC
	buf := make([]byte, 32)
	if e.Type != TYPE_INJECT_TOUCH_EVENT {
		log.Printf("Mismatch Event Type: %d\n", e.Type)
		return
	}
	// 2. 使用 Put 系列函数直接填充内存，速度极快
	buf[0] = e.Type                                    // Type
	buf[1] = e.Action                                  // Action
	binary.BigEndian.PutUint64(buf[2:10], e.PointerID) // PointerID (8 bytes)
	binary.BigEndian.PutUint32(buf[10:14], e.PosX)     // PosX (4 bytes)
	binary.BigEndian.PutUint32(buf[14:18], e.PosY)     // PosY (4 bytes)
	binary.BigEndian.PutUint16(buf[18:20], e.Width)    // Width (2 bytes)
	binary.BigEndian.PutUint16(buf[20:22], e.Height)   // Height (2 bytes)
	binary.BigEndian.PutUint16(buf[22:24], e.Pressure) // Pressure (2 bytes)
	binary.BigEndian.PutUint32(buf[24:28], e.Buttons)  // Buttons (4 bytes)
	binary.BigEndian.PutUint32(buf[28:32], e.Buttons)  // Buttons (4 bytes)

	// 3. 一次性发送
	_, err := da.controlConn.Write(buf)
	if err != nil {
		log.Printf("Error sending touch event: %v\n", err)
	}
}

func (da *DataAdapter) SendKeyEvent(e KeyEvent) {
	if da.controlConn == nil {
		return
	}

	buf := make([]byte, 14)

	buf[0] = TYPE_INJECT_KEYCODE                    // Type
	buf[1] = e.Action                               // Action
	binary.BigEndian.PutUint32(buf[2:6], e.KeyCode) // KeyCode (4 bytes)
	binary.BigEndian.PutUint32(buf[6:10], 0)        // Repeat (4 bytes)
	binary.BigEndian.PutUint32(buf[10:14], 0)       // Meta (4 bytes)

	_, err := da.controlConn.Write(buf)
	if err != nil {
		log.Printf("Error sending key event: %v\n", err)
	}
}

// 	_, err := da.controlConn.Write(buf)
// 	if err != nil {
// 		log.Printf("Error sending key event: %v\n", err)
// 	}
// }

func (da *DataAdapter) RotateDevice() {
	if da.controlConn == nil {
		return
	}
	log.Println("Sending Rotate Device command...")
	msg := []byte{TYPE_ROTATE_DEVICE}
	_, err := da.controlConn.Write(msg)
	if err != nil {
		log.Printf("Error sending rotate command: %v\n", err)
	}
}

func (da *DataAdapter) RequestKeyFrame() error {
	if da.controlConn == nil {
		return nil
	}
	da.keyFrameRequestMutex.Lock()
	defer da.keyFrameRequestMutex.Unlock()
	log.Printf("Last KeyFrame request time: %v Last Request KeyFrame time: %v", da.lastKeyFrameTime, da.lastRequestKeyFrameTime)
	if time.Since(da.lastRequestKeyFrameTime) < 2*time.Second {
		log.Println("⏳ KeyFrame request too frequent, use cached")
		da.keyFrameMutex.RLock()

		isH265 := da.VideoMeta.CodecID == "h265"
		hasSPS := len(da.LastSPS) > 0
		hasPPS := len(da.LastPPS) > 0
		hasIDR := len(da.LastIDR) > 0
		hasVPS := len(da.LastVPS) > 0

		if !hasSPS || !hasPPS || !hasIDR || (isH265 && !hasVPS) {
			da.keyFrameMutex.RUnlock()
			log.Println("⚠️ Cached keyframe data incomplete, skipping...")
			return nil
		}

		createCopyFromPool := func(src []byte) []byte {
			dst := da.VideoPayloadPool.Get().([]byte)
			if cap(dst) < len(src) {
				da.VideoPayloadPool.Put(dst)
				dst = make([]byte, len(src))
			}
			dst = dst[:len(src)]
			copy(dst, src)
			return dst
		}

		var vpsCopy, spsCopy, ppsCopy, idrCopy []byte
		if isH265 {
			vpsCopy = createCopyFromPool(da.LastVPS)
		}
		spsCopy = createCopyFromPool(da.LastSPS)
		ppsCopy = createCopyFromPool(da.LastPPS)
		idrCopy = createCopyFromPool(da.LastIDR)

		// Check freshness of IDR
		idrFresh := time.Since(da.LastIDRTime) < 500*time.Millisecond

		da.keyFrameMutex.RUnlock()

		go func() {
			timestamp := time.Now().Unix()
			if isH265 && vpsCopy != nil {
				da.VideoChan <- WebRTCFrame{Data: vpsCopy, Timestamp: timestamp}
			}
			da.VideoChan <- WebRTCFrame{Data: spsCopy, Timestamp: timestamp}
			da.VideoChan <- WebRTCFrame{Data: ppsCopy, Timestamp: timestamp}

			if idrFresh {
				da.VideoChan <- WebRTCFrame{Data: idrCopy, Timestamp: timestamp}
				log.Println("✅ Sent cached keyframe data (Fresh IDR)")
			} else {
				log.Println("✅ Sent cached keyframe data (Config only, IDR too old)")
				// If we don't send IDR, we should probably put the buffer back?
				// But createCopyFromPool allocates from pool.
				// The receiver of VideoChan is responsible for putting it back.
				// If we don't send it, we leak it (or rather, we hold it until GC if we didn't use pool, but we used pool).
				// Wait, if we don't send it to VideoChan, we MUST put it back manually.
				da.VideoPayloadPool.Put(idrCopy)
			}
		}()
		return nil
	}
	log.Println("⚡ Sending Request KeyFrame (Type 99)...")
	msg := []byte{ControlMsgTypeReqIDR}
	//<-da.VideoChan
	da.lastRequestKeyFrameTime = time.Now()
	_, err := da.controlConn.Write(msg)
	if err != nil {
		log.Printf("Error sending keyframe request: %v\n", err)
		return err
	}
	return nil
}
