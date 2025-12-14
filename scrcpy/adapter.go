package scrcpy

import (
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// type StreamChan struct {
// 	VideoChan        chan WebRTCVideoFrame
// 	AudioChan        chan WebRTCAudioFrame
// 	ControlChan      chan WebRTCControlFrame
// 	VideoPayloadPool sync.Pool
// }

type DataAdapter struct {
	// VideoChanMutex   sync.RWMutex
	// AudioChanMutex   sync.RWMutex
	VideoChan        chan WebRTCFrame
	AudioChan        chan WebRTCFrame
	ControlChan      chan WebRTCControlFrame
	VideoPayloadPool sync.Pool
	AudioPayloadPool sync.Pool

	DeviceName string
	VideoMeta  ScrcpyVideoMeta
	AudioMeta  ScrcpyAudioMeta

	// convertVideoPaused bool
	// convertAudioPaused bool

	videoConn   net.Conn
	audioConn   net.Conn
	controlConn net.Conn

	adbClient *ADBClient

	keyFrameRequestMutex    sync.Mutex
	lastKeyFrameTime        time.Time
	lastRequestKeyFrameTime time.Time

	keyFrameMutex sync.RWMutex // 保护 LastSPS, LastPPS, LastIDR
	LastVPS       []byte       // 新增：H.265 VPS
	LastSPS       []byte
	LastPPS       []byte
	LastIDR       []byte
	LastIDRTime   time.Time
	// LastPFrames   [][]byte
}

// 一个DataAdapter对应一个scrcpy实例，通过本地端口建立三个连接：视频、音频、控制
func NewDataAdapter(config map[string]string) (*DataAdapter, error) {
	var err error
	da := &DataAdapter{
		adbClient: NewADBClient(config["device_serial"]),
		// convertVideoPaused: false,
		// convertAudioPaused: false,

		VideoChan:   make(chan WebRTCFrame, 10),
		AudioChan:   make(chan WebRTCFrame, 10),
		ControlChan: make(chan WebRTCControlFrame, 10),
		VideoPayloadPool: sync.Pool{
			New: func() interface{} {
				// 预分配 512KB (根据你的 H264 码率调整)
				return make([]byte, 1024*1024)
			},
		},
		AudioPayloadPool: sync.Pool{
			New: func() interface{} {
				// 1KB 足够放下大多数 Opus 帧
				return make([]byte, 1024)
			},
		},
	}
	err = da.adbClient.Push(config["server_local_path"], config["server_remote_path"])
	if err != nil {
		log.Fatalf("设置 推送scrcpy-server失败: %v", err)
	}
	err = da.adbClient.Reverse("localabstract:scrcpy", "tcp:"+config["local_port"])
	if err != nil {
		log.Fatalf("设置 Reverse 隧道失败: %v", err)
		return nil, err
	}
	da.adbClient.StartScrcpyServer()
	listener, err := net.Listen("tcp", ":"+config["local_port"])
	if err != nil {
		log.Fatalf("监听端口失败: %v", err)
		return nil, err
	}
	defer listener.Close()
	conns := make([]net.Conn, 3)
	for i := 0; i < 3; i++ {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatalf("Accept 失败: %v", err)
			return nil, err
		}
		log.Println("Accept Connection", i)
		conns[i] = conn
	}

	for i, conn := range conns {
		// The target environment is ARM devices, read directly without buffering could be faster because of less memory copy
		// conn := comm.NewBufferedReadWriteCloser(_conn, 4096)
		switch i {
		case 0:
			// Device Metadata and Video
			err = da.readDeviceMeta(conn)
			if err != nil {
				log.Fatalln("Failed to read device metadata:", err)
				return nil, err
			}
			log.Printf("Connected Device: %s", da.DeviceName)

			da.assignConn(conn)
		case 1:
			da.assignConn(conn)
		case 2:
			// The third connection is always Control
			da.controlConn = conn
			log.Println("Scrcpy Control Connection Established")
		}
	}
	da.videoConn.(*net.TCPConn).SetReadBuffer(64 * 1024)
	da.audioConn.(*net.TCPConn).SetReadBuffer(16 * 1024)

	return da, nil
}

func (da *DataAdapter) Close() {
	if da.videoConn != nil {
		da.videoConn.Close()
	}
	if da.audioConn != nil {
		da.audioConn.Close()
	}
	if da.controlConn != nil {
		da.controlConn.Close()
	}
	da.adbClient.ReverseRemove("localabstract:scrcpy")
	da.adbClient.Stop()
	// close(da.VideoChan)
	// close(da.AudioChan)
}

// func (da *DataAdapter) PauseConvertVideo() {
// 	da.convertVideoPaused = true
// }

// func (da *DataAdapter) ResumeConvertVideo() {
// 	da.convertVideoPaused = false
// }

// func (da *DataAdapter) PauseConvertAudio() {
// 	da.convertAudioPaused = true
// }

// func (da *DataAdapter) ResumeConvertAudio() {
// 	da.convertAudioPaused = false
// }

func (da *DataAdapter) ShowDeviceInfo() {
	log.Printf("Device Name: %s", da.DeviceName)
	log.Printf("Video Codec: %s, Width: %d, Height: %d", da.VideoMeta.CodecID, da.VideoMeta.Width, da.VideoMeta.Height)
	log.Printf("Audio Codec: %s", da.AudioMeta.CodecID)
}

func (da *DataAdapter) StartConvertVideoFrame() {
	createCopy := func(src []byte) []byte {
		if len(src) == 0 {
			return nil
		}
		// A. 从池子拿（准备做复印件的纸）
		dst := da.VideoPayloadPool.Get().([]byte)

		// B. 容量检查
		// 如果池子里的纸太小（SPS通常很小，这种情况极少发生，但为了健壮性必须写）
		if cap(dst) < len(src) {
			// 把太小的还回去
			da.VideoPayloadPool.Put(dst)
			// 重新造个大的（这次 GC 无法避免，但仅限初始化阶段，无所谓）
			dst = make([]byte, len(src))
			log.Println("resize")
		}

		// C. 设定长度并拷贝
		dst = dst[:len(src)]
		copy(dst, src)
		return dst
	}
	go func() {
		var startCode = []byte{0x00, 0x00, 0x00, 0x01}
		var cachedVPS []byte
		var cachedSPS []byte
		var cachedPPS []byte

		var headerBuf [12]byte
		frame := &ScrcpyFrame{}

		isH265 := da.VideoMeta.CodecID == "h265"

		for {
			// read frame header
			if err := readScrcpyFrameHeader(da.videoConn, headerBuf[:], &frame.Header); err != nil {
				log.Println("Failed to read scrcpy frame header:", err)
				return
			}
			payloadBuf := da.VideoPayloadPool.Get().([]byte)
			if cap(payloadBuf) < int(frame.Header.Size) {
				da.VideoPayloadPool.Put(payloadBuf)
				payloadBuf = make([]byte, frame.Header.Size+1024)
				log.Println("Resized payload buffer for video frame")
				log.Println("size:  ", frame.Header.Size)
			}
			if _, err := io.ReadFull(da.videoConn, payloadBuf[:frame.Header.Size]); err != nil {
				log.Println("Failed to read video frame payload:", err)
				return
			}
			frameData := payloadBuf[:frame.Header.Size]

			// Parse NALUs to update cache
			parts := bytes.Split(frameData, startCode)
			for _, part := range parts {
				if len(part) == 0 {
					continue
				}

				var nalType uint8
				if isH265 {
					nalType = (part[0] >> 1) & 0x3F
				} else {
					nalType = part[0] & 0x1F
				}

				if isH265 {
					switch nalType {
					case 32: // VPS
						cachedVPS = createCopy(append(startCode, part...))
						da.keyFrameMutex.Lock()
						da.LastVPS = cachedVPS
						da.keyFrameMutex.Unlock()
					case 33: // SPS
						cachedSPS = createCopy(append(startCode, part...))
						da.keyFrameMutex.Lock()
						da.LastSPS = cachedSPS
						da.keyFrameMutex.Unlock()
					case 34: // PPS
						cachedPPS = createCopy(append(startCode, part...))
						da.keyFrameMutex.Lock()
						da.LastPPS = cachedPPS
						da.keyFrameMutex.Unlock()
					case 19, 20, 21: // IDR
						da.keyFrameMutex.Lock()
						da.LastIDR = createCopy(frameData) // Store full frame
						da.LastIDRTime = time.Now()
						da.keyFrameMutex.Unlock()
					}
				} else {
					switch nalType {
					case 7: // SPS
						cachedSPS = createCopy(append(startCode, part...))
						da.keyFrameMutex.Lock()
						da.LastSPS = cachedSPS
						da.keyFrameMutex.Unlock()
					case 8: // PPS
						cachedPPS = createCopy(append(startCode, part...))
						da.keyFrameMutex.Lock()
						da.LastPPS = cachedPPS
						da.keyFrameMutex.Unlock()
					case 5: // IDR
						da.keyFrameMutex.Lock()
						da.LastIDR = createCopy(frameData)
						da.LastIDRTime = time.Now()
						da.keyFrameMutex.Unlock()
					}
				}
			}

			// If it's a keyframe (but not a config frame itself), send cached config first
			if frame.Header.IsKeyFrame && !frame.Header.IsConfig {
				if isH265 && cachedVPS != nil {
					da.VideoChan <- WebRTCFrame{Data: createCopy(cachedVPS), Timestamp: int64(frame.Header.PTS)}
				}
				if cachedSPS != nil {
					da.VideoChan <- WebRTCFrame{Data: createCopy(cachedSPS), Timestamp: int64(frame.Header.PTS)}
				}
				if cachedPPS != nil {
					da.VideoChan <- WebRTCFrame{Data: createCopy(cachedPPS), Timestamp: int64(frame.Header.PTS)}
				}
			}

			// 这里的 Data 引用了 pool 中的内存，消费者用完必须 Put 回去
			webRTCFrame := WebRTCFrame{
				Data:      frameData,
				Timestamp: int64(frame.Header.PTS),
			}
			select {
			case da.VideoChan <- webRTCFrame:

			default:
				log.Println("Video channel full, waiting to send frame...")
				da.VideoChan <- webRTCFrame
			}
		}
	}()
}

func (da *DataAdapter) StartConvertAudioFrame() {
	go func() {
		var headerBuf [12]byte
		frame := &ScrcpyFrame{}
		for {
			// read frame header
			if err := readScrcpyFrameHeader(da.audioConn, headerBuf[:], &frame.Header); err != nil {
				log.Println("Failed to read scrcpy audio frame header:", err)
				return
			}
			// log.Printf("Audio Frame Timestamp: %v, Size: %v isConfig: %v\n", frame.Header.PTS, frame.Header.Size, frame.Header.IsConfig)
			payloadBuf := da.AudioPayloadPool.Get().([]byte)
			if cap(payloadBuf) < int(frame.Header.Size) {
				log.Println("current buf cap:", cap(payloadBuf))
				if cap(payloadBuf) >= 1024 {
					da.AudioPayloadPool.Put(payloadBuf)
				}
				payloadBuf = make([]byte, frame.Header.Size+1024)
				log.Println("Resized payload buffer for audio frame")
				log.Println("size:  ", frame.Header.Size)
			}
			// read frame payload
			n, _ := io.ReadFull(da.audioConn, payloadBuf[:frame.Header.Size])
			frameData := payloadBuf[:frame.Header.Size]

			if frame.Header.IsConfig {
				log.Println("Audio Config Frame Received")

				totalLen := 7 + 8 + int(n)
				configBuf := da.AudioPayloadPool.Get().([]byte)
				if cap(configBuf) < totalLen {
					if cap(configBuf) >= 1024 {
						da.AudioPayloadPool.Put(configBuf)
					}
					configBuf = make([]byte, 1024)
					if cap(configBuf) < totalLen {
						configBuf = make([]byte, totalLen+512)
					}
					log.Println("Resized config buffer for audio config frame")
				}
				configBuf = configBuf[:totalLen]
				copy(configBuf[0:7], []byte("AOPUSHC"))                   // Magic
				binary.LittleEndian.PutUint64(configBuf[7:15], uint64(n)) // Length
				copy(configBuf[15:], frameData)
				da.AudioChan <- WebRTCFrame{
					Data:      configBuf,
					Timestamp: int64(frame.Header.PTS),
				}
				da.AudioPayloadPool.Put(payloadBuf)
				continue
			}

			// 这里的 Data 引用了 pool 中的内存，消费者用完必须 Put 回去
			webRTCFrame := WebRTCFrame{
				Data:      frameData,
				Timestamp: int64(frame.Header.PTS),
			}
			select {
			case da.AudioChan <- webRTCFrame:

			default:
				log.Println("Audio channel full, waiting to send frame...")
				da.AudioChan <- webRTCFrame
			}
		}
	}()
}

func (da *DataAdapter) assignConn(conn net.Conn) error {
	codecID := ReadCodecID(conn)
	switch codecID {
	case "h264", "h265", "av1 ":
		da.videoConn = conn
		da.VideoMeta.CodecID = codecID
		err := da.readVideoMeta(conn)
		if err != nil {
			log.Fatalln("Failed to read video metadata:", err)
			return err
		}
		log.Println("Scrcpy Video Connection Established")
	case "aac ", "opus":
		da.audioConn = conn
		da.AudioMeta.CodecID = codecID
		log.Println("Audio Connection Established")
	default:
		da.controlConn = conn
		log.Println("Scrcpy Control Connection Established")
	}
	return nil
}

func (da *DataAdapter) readDeviceMeta(conn net.Conn) error {
	// 1. Device Name (64 bytes)
	nameBuf := make([]byte, 64)
	_, err := io.ReadFull(conn, nameBuf)
	if err != nil {
		return err
	}
	da.DeviceName = string(nameBuf)
	return nil
}

func ReadCodecID(conn net.Conn) string {
	// Codec ID (4 bytes)
	codecBuf := make([]byte, 4)
	if _, err := io.ReadFull(conn, codecBuf); err != nil {
		log.Println("Failed to read codec ID:", err)
		return ""
	}

	return string(codecBuf)
}

func (da *DataAdapter) readVideoMeta(conn net.Conn) error {
	// Width (4 bytes)
	// Height (4 bytes)
	// Codec 已经在外面读取过了，用于确认是哪个通道
	metaBuf := make([]byte, 8)
	if _, err := io.ReadFull(conn, metaBuf); err != nil {
		log.Println("Failed to read metadata:", err)
		return err
	}
	// 解析元数据
	da.VideoMeta.Width = binary.BigEndian.Uint32(metaBuf[0:4])
	da.VideoMeta.Height = binary.BigEndian.Uint32(metaBuf[4:8])

	return nil
}

func readScrcpyFrameHeader(conn net.Conn, headerBuf []byte, header *ScrcpyFrameHeader) error {
	if _, err := io.ReadFull(conn, headerBuf); err != nil {
		return err
	}

	ptsAndFlags := binary.BigEndian.Uint64(headerBuf[0:8])
	packetSize := binary.BigEndian.Uint32(headerBuf[8:12])

	// 提取标志位
	isConfig := (ptsAndFlags & 0x8000000000000000) != 0
	isKeyFrame := (ptsAndFlags & 0x4000000000000000) != 0

	// 提取PTS (低62位)
	pts := uint64(ptsAndFlags & 0x3FFFFFFFFFFFFFFF)
	header.IsConfig = isConfig
	header.IsKeyFrame = isKeyFrame
	header.PTS = pts
	header.Size = packetSize
	return nil
}
