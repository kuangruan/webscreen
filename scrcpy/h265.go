package scrcpy

import (
	"bytes"
	"fmt"
	"iter"
	"log"
	"time"
)

func (da *DataAdapter) GenerateWebRTCFrameH265(header ScrcpyFrameHeader, payload []byte) iter.Seq[WebRTCFrame] {
	return func(yield func(WebRTCFrame) bool) {
		var startCode = []byte{0x00, 0x00, 0x00, 0x01}

		if (payload[4]>>1)&0x3F == 32 {
			VPSData := []byte{}
			SPSData := []byte{}
			PPSData := []byte{}
			IDRData := []byte{}
			parts := bytes.Split(payload, startCode)
			for _, nal := range parts {
				if len(nal) == 0 {
					continue
				}
				nalType := (nal[0] >> 1) & 0x3F
				log.Printf("NALU Type: %d, size: %d", nalType, len(nal))
				switch nalType {
				case 32: // VPS
					VPSData = nal
					da.keyFrameMutex.Lock()
					da.LastVPS = VPSData
					da.keyFrameMutex.Unlock()
					// log.Println("VPS NALU processed, size:", len(VPSData))
				case 33: // SPS
					da.updateVideoMetaFromSPS(nal, "h265")
					SPSData = nal
					da.keyFrameMutex.Lock()
					da.LastSPS = SPSData
					da.keyFrameMutex.Unlock()
					// log.Println("SPS NALU processed, size:", len(SPSData))
				case 34: // PPS
					PPSData = nal
					da.keyFrameMutex.Lock()
					da.LastPPS = PPSData
					da.keyFrameMutex.Unlock()
					// log.Println("PPS NALU processed, size:", len(PPSData))
				case 19, 20, 21: // IDR
					IDRData = nal
					da.keyFrameMutex.Lock()
					da.LastIDR = IDRData
					da.LastIDRTime = time.Now()
					da.keyFrameMutex.Unlock()
					// log.Println("IDR NALU processed, size:", len(IDRData))
				}
			}
			// Yield Packets
			if len(VPSData) > 0 {
				if !yield(WebRTCFrame{Data: createCopy(VPSData, &da.PayloadPoolSmall), Timestamp: int64(header.PTS)}) {
					return
				}
			}
			if len(SPSData) > 0 {
				if !yield(WebRTCFrame{Data: createCopy(SPSData, &da.PayloadPoolSmall), Timestamp: int64(header.PTS)}) {
					return
				}
			}
			if len(PPSData) > 0 {
				if !yield(WebRTCFrame{Data: createCopy(PPSData, &da.PayloadPoolSmall), Timestamp: int64(header.PTS)}) {
					return
				}
			}
			if len(IDRData) > 0 {
				if !yield(WebRTCFrame{Data: createCopy(IDRData, &da.PayloadPoolLarge), Timestamp: int64(header.PTS), NotConfig: true}) {
					return
				}
			}
			log.Println("Sent H265 keyframe NALUs: VPS, SPS, PPS, IDR")
			return // 已经处理完所有NALU，返回
		}

		// If it's a keyframe, send cached config first
		if header.IsKeyFrame {
			da.keyFrameMutex.Lock()
			da.LastIDR = payload
			da.LastIDRTime = time.Now()
			da.keyFrameMutex.Unlock()

			if da.LastVPS != nil {
				if !yield(WebRTCFrame{Data: createCopy(da.LastVPS, &da.PayloadPoolSmall), Timestamp: int64(header.PTS)}) {
					return
				}
			}
			if da.LastSPS != nil {
				if !yield(WebRTCFrame{Data: createCopy(da.LastSPS, &da.PayloadPoolSmall), Timestamp: int64(header.PTS)}) {
					return
				}
			}
			if da.LastPPS != nil {
				if !yield(WebRTCFrame{Data: createCopy(da.LastPPS, &da.PayloadPoolSmall), Timestamp: int64(header.PTS)}) {
					return
				}
			}
		}

		if !yield(WebRTCFrame{
			Data:      payload,
			Timestamp: int64(header.PTS),
			NotConfig: true,
		}) {
			return
		}
	}
}

// ParseSPS_H265 解析 H.265 SPS NAL Unit (不包含 Start Code 00000001)
func ParseSPS_H265(sps []byte) (SPSInfo, error) {
	info := SPSInfo{}

	if len(sps) < 2 {
		return info, fmt.Errorf("SPS data too short")
	}

	// 1. 去除防竞争码 (Emulation Prevention Bytes: 00 00 03 -> 00 00)
	rbsp := removeEmulationPreventionBytes(sps)

	// 2. 初始化 BitReader
	br := newBitReader(rbsp)

	// 3. 解析 NAL Header (2 bytes)
	// H.265 NAL Header: F(1) + Type(6) + LayerId(6) + TID(3)
	br.ReadBits(1)            // forbidden_zero_bit
	nalType := br.ReadBits(6) // nal_unit_type
	br.ReadBits(6)            // nuh_layer_id
	br.ReadBits(3)            // nuh_temporal_id_plus1

	// SPS NAL Type 应该是 33 (0x21)
	if nalType != 33 {
		return info, fmt.Errorf("not an H.265 SPS NAL unit (type: %d)", nalType)
	}

	// 4. 解析 SPS Body
	// sps_video_parameter_set_id: u(4)
	br.ReadBits(4)

	// sps_max_sub_layers_minus1: u(3)
	maxSubLayersMinus1 := br.ReadBits(3)

	// sps_temporal_id_nesting_flag: u(1)
	br.ReadBits(1)

	// --- Profile Tier Level (PTL) ---
	// 这部分非常关键，必须正确跳过 sub-layers 才能读到后面的尺寸信息
	profile, tier, level, err := parseProfileTierLevel(br, maxSubLayersMinus1)
	if err != nil {
		return info, err
	}
	info.Profile = profile
	info.Level = fmt.Sprintf("%.1f", float32(level)/30.0)
	if tier == 1 {
		info.Tier = "High"
	} else {
		info.Tier = "Main"
	}

	// sps_seq_parameter_set_id: ue(v)
	br.ReadUE()

	// chroma_format_idc: ue(v)
	chromaFormatIDC := br.ReadUE()
	info.ChromaFormat = chromaFormatIDC
	if chromaFormatIDC == 3 {
		br.ReadBits(1) // separate_colour_plane_flag
	}

	// pic_width_in_luma_samples: ue(v)
	width := br.ReadUE()

	// pic_height_in_luma_samples: ue(v)
	height := br.ReadUE()

	// conformance_window_flag: u(1) (用于裁剪，例如 1920x1088 -> 1920x1080)
	conformanceWindowFlag := br.ReadBits(1)

	confWinLeftOffset := uint32(0)
	confWinRightOffset := uint32(0)
	confWinTopOffset := uint32(0)
	confWinBottomOffset := uint32(0)

	if conformanceWindowFlag == 1 {
		confWinLeftOffset = br.ReadUE()
		confWinRightOffset = br.ReadUE()
		confWinTopOffset = br.ReadUE()
		confWinBottomOffset = br.ReadUE()
	}

	// 计算实际显示分辨率
	// H.265 中裁剪单位取决于 Chroma Format
	// Chroma Format IDC: 0=Monochrome, 1=4:2:0, 2=4:2:2, 3=4:4:4
	subWidthC := uint32(1)
	subHeightC := uint32(1)

	if chromaFormatIDC == 1 { // 4:2:0 (最常见)
		subWidthC = 2
		subHeightC = 2
	} else if chromaFormatIDC == 2 { // 4:2:2
		subWidthC = 2
		subHeightC = 1
	}
	// 4:4:4 (3) 都是 1

	info.Width = width - (confWinLeftOffset+confWinRightOffset)*subWidthC
	info.Height = height - (confWinTopOffset+confWinBottomOffset)*subHeightC

	return info, nil
}

// parseProfileTierLevel 解析 PTL 结构，这对跳过比特位至关重要
func parseProfileTierLevel(br *bitReader, maxSubLayersMinus1 uint32) (uint8, uint8, uint8, error) {
	// general_profile_space: u(2)
	br.ReadBits(2)
	// general_tier_flag: u(1)
	tierFlag := uint8(br.ReadBits(1))
	// general_profile_idc: u(5)
	profileIDC := uint8(br.ReadBits(5))

	// general_profile_compatibility_flag: u(32)
	br.ReadBits(32)

	// general_constraint_indicator_flags: u(48)
	br.ReadBits(32)
	br.ReadBits(16)

	// general_level_idc: u(8)
	levelIDC := uint8(br.ReadBits(8))

	// 处理 sub_layers 的存在标志
	subLayerProfilePresentFlag := make([]bool, maxSubLayersMinus1)
	subLayerLevelPresentFlag := make([]bool, maxSubLayersMinus1)

	for i := uint32(0); i < maxSubLayersMinus1; i++ {
		subLayerProfilePresentFlag[i] = br.ReadBits(1) == 1
		subLayerLevelPresentFlag[i] = br.ReadBits(1) == 1
	}

	if maxSubLayersMinus1 > 0 {
		for i := uint32(maxSubLayersMinus1); i < 8; i++ {
			br.ReadBits(2) // reserved_zero_2bits
		}
	}

	// 跳过 sub_layers 的数据
	for i := uint32(0); i < maxSubLayersMinus1; i++ {
		if subLayerProfilePresentFlag[i] {
			br.ReadBits(2)  // sub_layer_profile_space
			br.ReadBits(1)  // sub_layer_tier_flag
			br.ReadBits(5)  // sub_layer_profile_idc
			br.ReadBits(32) // sub_layer_profile_compatibility_flag
			br.ReadBits(32) // sub_layer_constraint_indicator_flags (part 1)
			br.ReadBits(16) // sub_layer_constraint_indicator_flags (part 2)
		}
		if subLayerLevelPresentFlag[i] {
			br.ReadBits(8) // sub_layer_level_idc
		}
	}

	return profileIDC, tierFlag, levelIDC, nil
}

// --- BitReader 工具 ---

type bitReader struct {
	data   []byte
	offset int // 当前 bit 偏移量
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data, offset: 0}
}

// ReadBits 读取 n 个 bit
func (r *bitReader) ReadBits(n int) uint32 {
	var res uint32 = 0
	for i := 0; i < n; i++ {
		byteOffset := r.offset / 8
		bitOffset := 7 - (r.offset % 8)
		r.offset++

		if byteOffset >= len(r.data) {
			continue // Prevent panic, return 0
		}

		bit := (r.data[byteOffset] >> bitOffset) & 1
		res = (res << 1) | uint32(bit)
	}
	return res
}

// ReadUE 读取指数哥伦布编码 (Unsigned Exp-Golomb)
func (r *bitReader) ReadUE() uint32 {
	leadingZeros := 0
	for {
		// 读取 1 bit，直到读到 1
		bit := r.ReadBits(1)
		if bit == 1 {
			break
		}
		leadingZeros++
		// 简单的防死循环保护
		if leadingZeros > 32 {
			return 0
		}
	}

	// 读后面的 leadingZeros 位
	val := r.ReadBits(leadingZeros)
	return (1 << leadingZeros) - 1 + val
}

// removeEmulationPreventionBytes 移除 H.264/H.265 中的防竞争码 0x03
func removeEmulationPreventionBytes(data []byte) []byte {
	// 如果不包含 00 00 03，直接返回
	if !bytes.Contains(data, []byte{0, 0, 3}) {
		return data
	}

	buf := make([]byte, 0, len(data))
	i := 0
	for i < len(data) {
		if i+2 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 3 {
			buf = append(buf, 0, 0)
			i += 3
		} else {
			buf = append(buf, data[i])
			i++
		}
	}
	return buf
}
