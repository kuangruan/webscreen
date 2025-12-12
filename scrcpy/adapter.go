package scrcpy

import (
	"encoding/binary"
	"io"
	"log"
	"net"
	"sync"
)

type DataAdapter struct {
	VideoChan   chan WebRTCVideoFrame
	AudioChan   chan WebRTCAudioFrame
	ControlChan chan WebRTCControlFrame

	DeviceName string
	VideoMeta  ScrcpyVideoMeta
	AudioMeta  ScrcpyAudioMeta

	videoConn   net.Conn
	audioConn   net.Conn
	controlConn net.Conn

	VideoPayloadPool sync.Pool
}

func NewDataAdapter(conns []net.Conn) *DataAdapter {
	da := &DataAdapter{}
	var err error
	for i, conn := range conns {
		if tcpConn, ok := conn.(*net.TCPConn); ok {
			tcpConn.SetReadBuffer(2 * 1024 * 1024)
		}
		// The target environment is ARM devices, read directly without buffering could be faster because of less memory copy
		// conn := comm.NewBufferedReadWriteCloser(_conn, 4096)
		switch i {
		case 0:
			// Device Metadata and Video
			err = da.readDeviceMeta(conn)
			if err != nil {
				log.Fatalln("Failed to read device metadata:", err)
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
	da.VideoChan = make(chan WebRTCVideoFrame, 100)
	da.AudioChan = make(chan WebRTCAudioFrame, 100)
	da.ControlChan = make(chan WebRTCControlFrame, 100)

	da.VideoPayloadPool = sync.Pool{
		New: func() interface{} {
			// 预分配 1MB (根据你的 H264 码率调整)
			return make([]byte, 1024*1024)
		},
	}
	return da
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
	// close(da.VideoChan)
	// close(da.AudioChan)
}

func (da *DataAdapter) StartConvertVideoFrame() {
	go func() {
		var headerBuf [12]byte
		frame := &ScrcpyVideoFrame{}
		for {
			// read frame header
			if err := readScrcpyFrameHeader(da.videoConn, headerBuf[:], &frame.Header); err != nil {
				log.Println("Failed to read scrcpy frame header:", err)
				return
			}
			payloadBuf := da.VideoPayloadPool.Get().([]byte)
			if cap(payloadBuf) < int(frame.Header.Size) {
				da.VideoPayloadPool.Put(&payloadBuf)
				payloadBuf = make([]byte, frame.Header.Size+1024)
			}
			if _, err := io.ReadFull(da.videoConn, payloadBuf[:frame.Header.Size]); err != nil {
				log.Println("Failed to read video frame payload:", err)
				return
			}
			// 这里的 Data 引用了 pool 中的内存，消费者用完必须 Put 回去
			webRTCFrame := WebRTCVideoFrame{
				Data:      payloadBuf[:frame.Header.Size],
				Timestamp: int64(frame.Header.PTS),
			}
			da.VideoChan <- webRTCFrame
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

func readScrcpyFrameHeader(conn net.Conn, headerBuf []byte, header *ScrcpyVideoFrameHeader) error {
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
