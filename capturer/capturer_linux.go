package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

func waitTCP() net.Conn {
	var err error
	var conn net.Conn
	for {
		conn, err = net.Dial("tcp", "127.0.0.1:27184")
		if err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	return conn
}

// 配置参数
const (
	DisplayNum = ":99"
	Resolution = "1920x1080"
	BitDepth   = "24" // 24位色深
)

func main() {
	// 1. 启动 Xvfb 虚拟显示器
	log.Printf("正在启动虚拟显示器 Xvfb %s (%sx%s)...", DisplayNum, Resolution, BitDepth)

	// Xvfb 命令: Xvfb :99 -ac -screen 0 1920x1080x24
	// -nolisten tcp: 为了安全，不监听 TCP 端口，只走 Unix Socket
	xvfbCmd := exec.Command("Xvfb", DisplayNum, "-ac", "-screen", "0", fmt.Sprintf("%sx%s", Resolution, BitDepth), "-nolisten", "tcp")

	// 将 Xvfb 的输出重定向到空，或者是 os.Stdout 以便调试
	// xvfbCmd.Stdout = os.Stdout
	xvfbCmd.Stderr = os.Stderr

	if err := xvfbCmd.Start(); err != nil {
		log.Fatalf("无法启动 Xvfb: %v\n请确保已安装: sudo apt install xvfb", err)
	}

	// 定义清理函数：用于杀死 Xvfb 进程
	cleanup := func() {
		log.Println("正在清理资源，关闭虚拟显示器...")
		if xvfbCmd.Process != nil {
			xvfbCmd.Process.Kill()
			xvfbCmd.Wait() // 等待进程彻底结束
		}
		// 清理锁文件（防止下次启动报错），虽然 Xvfb 正常退出会自动清理，但为了保险
		os.Remove("/tmp/.X11-unix/X" + DisplayNum[1:])
		os.Remove("/tmp/.X" + DisplayNum[1:] + "-lock")
		log.Println("清理完成，程序退出。")
	}

	// 2. 监听 Ctrl+C，确保退出时执行清理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan // 阻塞直到收到信号
		cleanup()
		os.Exit(0)
	}()

	// 确保 main 函数正常结束时也清理
	defer cleanup()

	// 等待 1 秒让 Xvfb 初始化完成
	time.Sleep(1 * time.Second)

	// ---------------------------------------------------------
	// 这里可以启动想在虚拟屏幕里运行的程序
	// 例如：启动一个简单的窗口管理器或浏览器
	// go runMyApp(DisplayNum)
	// ---------------------------------------------------------

	// 3. 连接服务端
	conn := waitTCP()
	defer conn.Close()

	// ==================== 新增等待逻辑 ====================
	// 等待 Xvfb 的 Socket 文件生成，最多等 5 秒
	socketPath := "/tmp/.X11-unix/X" + DisplayNum[1:] // 拼接出 /tmp/.X11-unix/X99
	log.Printf("正在等待 Xvfb 初始化 (Socket: %s)...", socketPath)

	xvfbReady := false
	for i := 0; i < 50; i++ { // 50 * 100ms = 5秒
		if _, err := os.Stat(socketPath); err == nil {
			xvfbReady = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !xvfbReady {
		log.Fatal("Xvfb 启动超时！请检查是否安装了 xvfb (sudo apt install xvfb) 或是否有权限")
	}

	// 为了保险，Socket文件创建后，X Server可能还需要几十毫秒才能真正 accept 连接
	time.Sleep(500 * time.Millisecond)
	log.Println("Xvfb 已就绪！")
	// ======================================================

	inputCtrl, err := NewInputController(DisplayNum) // DisplayNum = ":99"
	if err != nil {
		log.Println("警告：无法连接 X Server 初始化输入控制:", err)
	} else {
		defer inputCtrl.Close()
		log.Println("输入控制器已就绪")
	}

	// 注意：这里的 conn 是全双工的，Write 用于推流，Read 用于接收指令
	go func() {
		// 定义协议常量，与发送端保持一致
		const (
			EventTypeMouse = 0x01
			PacketSize     = 14 // 1(Type) + 1(Action) + 4(X) + 4(Y) + 4(Btn)
		)

		// 预分配一个小 buffer 用于读取头部或完整包
		packet := make([]byte, 14)

		for {
			_, err := io.ReadFull(conn, packet)
			if err != nil {
				log.Println("控制连接断开或读取错误:", err)
				return
			}
			eventType := packet[0]
			log.Printf("收到事件类型: 0x%X", eventType)
			switch eventType {
			case EventTypeMouse:
				// 2. 如果是鼠标事件，读取剩余的 13 字节
				payload := packet[1:PacketSize]
				_, err := io.ReadFull(conn, payload)
				if err != nil {
					log.Println("读取鼠标数据包失败:", err)
					return
				}

				// 3. 解析数据 (BigEndian)
				action := payload[0]
				x := binary.BigEndian.Uint32(payload[1:5])
				y := binary.BigEndian.Uint32(payload[5:9])
				buttons := binary.BigEndian.Uint32(payload[9:13])
				log.Printf("收到鼠标事件: Action=%d, X=%d, Y=%d, Buttons=0x%X\n", action, x, y, buttons)

				if inputCtrl == nil {
					log.Println("输入控制器未初始化，无法处理鼠标事件")
					continue
				}

				// 4. 执行控制逻辑
				// 注意：这里需要把 uint32 转为 int16 传给 InputController
				inputCtrl.HandleMouseEvent(action, int16(x), int16(y), buttons)

			default:
				log.Printf("收到未知事件类型: 0x%X", eventType)
				// 如果有变长包，这里如果不处理会导致后续数据错乱
				// 建议实现一个通用的长度头，但在目前定长场景下，这样够用了
			}
		}
	}()

	log.Println("连接成功，开始 FFmpeg 推流...")

	// 4. 启动 FFmpeg 抓取该虚拟屏幕
	ffmpegCmd := exec.Command("ffmpeg",
		"-f", "x11grab",
		"-framerate", "60",
		"-video_size", Resolution, // 使用定义的变量
		"-i", DisplayNum, // 连到我们刚创建的 :99

		// 编码参数
		"-c:v", "h264_rkmpp", // 如果在 PC 上跑，改成 libx264
		"-b:v", "6M",
		"-maxrate", "8M",
		"-g", "60",
		"-bf", "0",
		"-preset", "ultrafast",
		"-tune", "zerolatency",

		"-f", "h264",
		"-",
	)

	// 注入 DISPLAY 变量
	ffmpegCmd.Env = append(os.Environ(), fmt.Sprintf("DISPLAY=%s", DisplayNum))
	ffmpegCmd.Stderr = os.Stderr // 错误日志打印出来

	stdout, err := ffmpegCmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}

	if err := ffmpegCmd.Start(); err != nil {
		log.Fatal("FFmpeg 启动失败:", err)
	}

	// 5. 数据发送循环
	scanner := bufio.NewScanner(stdout)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 50*1024*1024)
	scanner.Split(splitNALU)

	header := make([]byte, 12)

	for scanner.Scan() {
		nalData := scanner.Bytes()
		if len(nalData) == 0 {
			continue
		}

		pts := uint64(time.Now().UnixNano() / 1e3)
		binary.BigEndian.PutUint64(header[0:8], pts)
		binary.BigEndian.PutUint32(header[8:12], uint32(len(nalData)))

		if _, err := conn.Write(header); err != nil {
			log.Println("网络发送错误:", err)
			break
		}
		if _, err := conn.Write(nalData); err != nil {
			log.Println("网络发送错误:", err)
			break
		}
	}

	// 循环结束后（通常是 FFmpeg 退出或网络断开），由 defer cleanup() 负责收尾
}

// splitNALU 是 bufio.SplitFunc 的实现，用于切分 H.264 Annex B 流
// 它会返回包含起始码（00 00 00 01）在内的完整 NALU
func splitNALU(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	// 查找起始码前缀 00 00 01
	// H.264 起始码可能是 00 00 01 (3 bytes) 或 00 00 00 01 (4 bytes)
	// 我们主要查找 00 00 01，然后判断前面是否还有一个 0

	// start 变量记录当前包的起始位置（包含起始码）
	start := 0

	// 如果数据开头不是 00 00 01 或 00 00 00 01，说明可能有些垃圾数据或上一包的残余（理论上不应发生）
	// 但为了健壮性，我们可以先定位到第一个起始码
	firstStart := bytes.Index(data, []byte{0, 0, 1})
	if firstStart == -1 {
		if atEOF {
			return len(data), nil, nil // 丢弃无法解析的数据
		}
		return 0, nil, nil // 等待更多数据
	}

	// 调整 start 位置，包含前导的 00 (如果是 4 字节起始码)
	if firstStart > 0 && data[firstStart-1] == 0 {
		start = firstStart - 1
	} else {
		start = firstStart
	}

	// 从 start + 3 开始查找*下一个*起始码
	// +3 是为了跳过当前的 00 00 01
	nextStart := bytes.Index(data[start+3:], []byte{0, 0, 1})

	if nextStart != -1 {
		// 找到了下一个起始码的位置（相对于 data[start+3:]）
		// 绝对位置是 start + 3 + nextStart
		end := start + 3 + nextStart

		// 检查下一个起始码前面是否还有一个 0 (构成 00 00 00 01)
		if data[end-1] == 0 {
			end--
		}

		// 返回当前完整的 NALU (从 start 到 end)
		// advance 设为 end，表示我们消费了这么多数据
		return end, data[start:end], nil
	}

	// 如果没找到下一个起始码，且已经 EOF，则剩余所有数据就是一个 NALU
	if atEOF {
		return len(data), data[start:], nil
	}

	// 如果没找到下一个起始码，且未 EOF，则请求更多数据
	// 注意：这里如果不返回 start 之前的垃圾数据，会导致 buffer 堆积，
	// 所以如果 start > 0，我们应该先丢弃 start 之前的垃圾
	if start > 0 {
		return start, nil, nil
	}

	return 0, nil, nil
}

func getNalType(data []byte) byte {
	// 简单的辅助函数，用于日志，需跳过起始码
	// 找到第一个非0字节（通常是1），下一字节就是 header
	for i := 0; i < len(data)-1; i++ {
		if data[i] == 0 && data[i+1] == 1 {
			if i+2 < len(data) {
				return data[i+2] & 0x1F
			}
		}
	}
	return 0
}
