package linuxX11Driver

import (
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"net"
	"time"
	"webscreen/sdriver"
	"webscreen/sdriver/comm"
)

// sudo killall Xvfb
type LinuxDriver struct {
	videoChan   chan sdriver.AVBox
	videoBuffer *comm.LinearBuffer
	conn        net.Conn

	ip       string
	user     string
	password string
}

// 简单的 Header 定义，对应发送端的结构
type Header struct {
	PTS  uint64
	Size uint32
}

func New(ip, user string) *LinuxDriver {
	d := &LinuxDriver{
		videoChan: make(chan sdriver.AVBox, 10), // 适当增大缓冲防止阻塞
		ip:        ip,
		user:      user,
		// password
	}
	d.videoBuffer = comm.NewLinearBuffer(16 * 1024 * 1024)
	return d
}
func (d *LinuxDriver) Start() {
	// 启动视频监听
	videoListener, err := net.Listen("tcp", ":27184")
	if err != nil {
		log.Println("Failed to start video listener:", err)
	}
	go d.handleConnection(videoListener)

	log.Println("LinuxDriver started, listening for connections...")
}

// Start, GetReceivers 等方法保持不变...
// 仅重写 handleConnection

func (d *LinuxDriver) handleConnection(l net.Listener) {
	conn, err := l.Accept()
	if err != nil {
		log.Println("Accept error:", err)
		return
	}
	d.conn = conn
	defer conn.Close()
	defer l.Close() // 只接受一个连接后就关闭监听，或者按需调整

	log.Println("Capturer connected")

	headerBuf := make([]byte, 12)

	for {
		// 1. 读取固定长度的 Header (12 bytes)
		if _, err := io.ReadFull(conn, headerBuf); err != nil {
			log.Println("Failed to read header:", err)
			return
		}

		pts := binary.BigEndian.Uint64(headerBuf[0:8])
		size := binary.BigEndian.Uint32(headerBuf[8:12])

		// 2. 准备 payload 缓冲区
		// 确保缓冲区够大
		payloadBuf := d.videoBuffer.Get(int(size))

		// 3. 读取完整的 NALU Payload
		if _, err := io.ReadFull(conn, payloadBuf); err != nil {
			log.Println("Failed to read payload:", err)
			return
		}

		// 此时 payloadBuf 包含 Annex B 格式数据 (00 00 00 01 XX XX ...)
		// 目标：剥离起始码，只保留 NAL Unit Header + Data

		// 查找起始码结束的位置
		// 起始码通常是 00 00 01 或 00 00 00 01
		startCodeEnd := 0
		if len(payloadBuf) > 4 && payloadBuf[0] == 0 && payloadBuf[1] == 0 && payloadBuf[2] == 0 && payloadBuf[3] == 1 {
			startCodeEnd = 4
		} else if len(payloadBuf) > 3 && payloadBuf[0] == 0 && payloadBuf[1] == 0 && payloadBuf[2] == 1 {
			startCodeEnd = 3
		} else {
			// 异常情况：没有标准起始码，可能数据错乱，或者发送端已经是 AVCC 格式？
			// 这里假设必须有 Annex B 起始码
			log.Printf("Warning: Invalid start code in NALU of size %d", size)
			continue
		}

		// 真正的 NAL 数据（不含起始码）
		nalData := payloadBuf[startCodeEnd:]

		if len(nalData) == 0 {
			continue
		}

		// 解析 NAL Header (第一个字节)
		nalHeader := nalData[0]
		nalType := nalHeader & 0x1F

		isKeyFrame := false
		isConfig := false

		switch nalType {
		case 7: // SPS
			isConfig = true
		case 8: // PPS
			isConfig = true
		case 5: // IDR (关键帧)
			isKeyFrame = true
		}

		// log.Printf("Recv NAL: type=%d, len=%d, isKey=%v", nalType, len(nalData), isKeyFrame)

		// 4. 发送 AVBox
		d.videoChan <- sdriver.AVBox{
			Data:       nalData, // 这里的切片引用的是 videoBuffer 的底层数组，注意生命周期
			PTS:        time.Duration(pts) * time.Microsecond,
			IsKeyFrame: isKeyFrame,
			IsConfig:   isConfig,
		}
	}
}

// ... 其他方法保持不变

// 实现 sdriver.SDriver 接口的其他方法
func (d *LinuxDriver) GetReceivers() (<-chan sdriver.AVBox, <-chan sdriver.AVBox, <-chan sdriver.Event) {
	return d.videoChan, nil, nil
}

func (d *LinuxDriver) SendEvent(event sdriver.Event) error {
	// log.Printf("X11Driver: Sending event type %T", event)
	buf := new(bytes.Buffer)

	// 常量定义 (需确保与 sdriver 包一致，或直接使用字面量)
	const (
		PacketTypeKey   = 0x00
		PacketTypeMouse = 0x01
	)

	switch v := event.(type) {

	case *sdriver.MouseEvent:
		buf.WriteByte(PacketTypeMouse) // Header: 0x01

		// Payload
		buf.WriteByte(v.Action)                        // [0] Action
		binary.Write(buf, binary.BigEndian, v.PosX)    // [1-4] X (或 DeltaX)
		binary.Write(buf, binary.BigEndian, v.PosY)    // [5-8] Y (或 DeltaY)
		binary.Write(buf, binary.BigEndian, v.Buttons) // [9-12] Buttons

		// 填充滚轮数据 (对应 TouchEvent 里的 int16(0))
		// 注意：需确保结构体里是 int16，或者在这里强转
		binary.Write(buf, binary.BigEndian, int16(v.WheelDeltaX)) // [13-14] Wheel X
		binary.Write(buf, binary.BigEndian, int16(v.WheelDeltaY)) // [15-16] Wheel Y
	// =================================================================
	// Case 1: 触摸事件 -> 鼠标包
	// 接收端 Payload 长度: 16 bytes
	// 结构: [Action 1][X 4][Y 4][Btn 4][Wheel X][Wheel Y]
	// =================================================================
	case *sdriver.TouchEvent:
		buf.WriteByte(PacketTypeMouse) // Header: 0x01

		// Payload (16 bytes)
		buf.WriteByte(v.Action)                        // [0] Action
		binary.Write(buf, binary.BigEndian, v.PosX)    // [1-4] X
		binary.Write(buf, binary.BigEndian, v.PosY)    // [5-8] Y
		binary.Write(buf, binary.BigEndian, v.Buttons) // [9-12] Buttons
		binary.Write(buf, binary.BigEndian, int16(0))  // [13-14] Wheel (触摸无滚轮)
		binary.Write(buf, binary.BigEndian, int16(0))  // [15-16] Padding (关键！补齐第16字节)

	// =================================================================
	// Case 2: 滚动事件 -> 鼠标包
	// =================================================================
	case *sdriver.ScrollEvent:
		buf.WriteByte(PacketTypeMouse) // Header: 0x01

		// Payload (16 bytes)
		buf.WriteByte(0)                               // [0] Action (滚动视为 Move)
		binary.Write(buf, binary.BigEndian, v.PosX)    // [1-4] X
		binary.Write(buf, binary.BigEndian, v.PosY)    // [5-8] Y
		binary.Write(buf, binary.BigEndian, v.Buttons) // [9-12] Buttons
		// [13-14] Wheel (将 HScroll 转为 int16)
		binary.Write(buf, binary.BigEndian, int16(v.HScroll))
		// [15-16] Wheel (将 VScroll 转为 int16)
		binary.Write(buf, binary.BigEndian, int16(v.VScroll))

	// =================================================================
	// Case 3: 键盘事件 -> 键盘包
	// 接收端 Payload 长度: 5 bytes
	// 结构: [Action 1][KeyCode 4]
	// =================================================================
	case *sdriver.KeyEvent:
		buf.WriteByte(PacketTypeKey) // Header: 0x00

		// Payload (5 bytes)
		buf.WriteByte(v.Action)                        // [0] Action
		binary.Write(buf, binary.BigEndian, v.KeyCode) // [1-4] KeyCode

	// 其他事件直接忽略
	default:

		return nil
	}

	// 发送数据
	if buf.Len() > 0 {
		_, err := d.conn.Write(buf.Bytes())
		return err
	}
	return nil
}
func (d *LinuxDriver) Pause() {}

func (d *LinuxDriver) RequestIDR(firstFrame bool) {
}

func (d *LinuxDriver) Capabilities() sdriver.DriverCaps {
	return sdriver.DriverCaps{
		CanAudio:     false,
		CanVideo:     true,
		CanControl:   true,
		CanClipboard: false,
		CanUHID:      false,
		IsLinux:      true,
	}
}

// CodecInfo() (videoCodec string, audioCodec string)
func (d *LinuxDriver) MediaMeta() sdriver.MediaMeta {
	return sdriver.MediaMeta{
		Width:        1920,
		Height:       1080,
		VideoCodecID: "h264",
		AudioCodecID: "",
	}
}
func (d *LinuxDriver) Stop() {}
