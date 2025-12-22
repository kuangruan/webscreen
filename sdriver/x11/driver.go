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
	// 1. 类型断言：判断是不是触摸/鼠标事件
	// 注意：你的 log 显示传入的是 *TouchEvent 指针
	touch, ok := event.(*sdriver.TouchEvent)
	if !ok {
		// 如果是指针不行，试试值类型 (取决于上层调用)
		if val, ok := event.(sdriver.TouchEvent); ok {
			touch = &val
		} else {
			// 暂不支持其他类型
			return nil
		}
	}

	// 2. 构造二进制包
	buf := new(bytes.Buffer)

	// [Byte 0] Event Type (Mouse/Touch 统一按 Mouse 处理)
	// 你的定义里 EVENT_TYPE_MOUSE = 0x01
	buf.WriteByte(byte(sdriver.EVENT_TYPE_MOUSE))

	// [Byte 1] Action
	buf.WriteByte(touch.Action)

	// [Byte 2-5] PosX (BigEndian)
	binary.Write(buf, binary.BigEndian, touch.PosX)

	// [Byte 6-9] PosY (BigEndian)
	binary.Write(buf, binary.BigEndian, touch.PosY)

	// [Byte 10-13] Buttons (BigEndian)
	binary.Write(buf, binary.BigEndian, touch.Buttons)

	// 3. 发送数据 (总共 14 字节)
	_, err := d.conn.Write(buf.Bytes())
	return err
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
