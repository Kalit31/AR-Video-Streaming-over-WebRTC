package client

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/asticode/go-astiav"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

// nolint: gochecknoglobals
var (
	inputFormatContext *astiav.FormatContext

	decodeCodecContext *astiav.CodecContext
	decodePacket       *astiav.Packet
	decodeFrame        *astiav.Frame
	videoStream        *astiav.Stream

	encodeCodecContext   *astiav.CodecContext
	encodePacket         *astiav.Packet

	softwareScaleContext *astiav.SoftwareScaleContext
	scaledFrame          *astiav.Frame

	softwareScaleContext2 *astiav.SoftwareScaleContext
	scaledFrame2          *astiav.Frame

	processedFrame *astiav.Frame

	filterFrame       *astiav.Frame
	filterGraph       *astiav.FilterGraph
	brightnessFilter  *astiav.FilterContext
	buffersinkCtx    *astiav.FilterContext
	buffersrcCtx     *astiav.FilterContext

	pts int64
	err error
)

const h264FrameDuration = time.Millisecond * 20

func writeH264ToTrackAR(track *webrtc.TrackLocalStaticSample) {
	astiav.RegisterAllDevices()

	initTestSrc()
	initFilters() 
	defer freeVideoCoding()

	conn, err := net.Dial("tcp", "localhost:5005")
    if err != nil {
        panic(err)
    }
    defer conn.Close()

	ticker := time.NewTicker(h264FrameDuration)
	for ; true; <-ticker.C {
		if err = inputFormatContext.ReadFrame(decodePacket); err != nil {
			if errors.Is(err, astiav.ErrEof) {
				break
			}
			panic(err)
		}
		decodePacket.RescaleTs(videoStream.TimeBase(), decodeCodecContext.TimeBase())

		if err = decodeCodecContext.SendPacket(decodePacket); err != nil {
			panic(err)
		}
	
		for {
			if err = decodeCodecContext.ReceiveFrame(decodeFrame); err != nil {
				if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
					fmt.Println("Error while receiving decoded framed: ", err)
					break
				}
				panic(err)
			}

			initVideoEncoding()

			// Scale the video
			if err = softwareScaleContext.ScaleFrame(decodeFrame, scaledFrame); err != nil {
				panic(err)
			}

			// We don't care about the PTS, but encoder complains if unset
			pts++
			scaledFrame.SetPts(pts)

			processedFrame, err = sendFrameToPython(conn, scaledFrame)
			if(err != nil){
				fmt.Println("ERROR WHILE SENDING FRAME TO PYTHON: ", err)
			}

			if err = softwareScaleContext2.ScaleFrame(processedFrame, scaledFrame2); err != nil {
				panic(err)
			}

			// We don't care about the PTS, but encoder complains if unset
			pts++
			scaledFrame2.SetPts(pts)
			
			// fmt.Println("scaledFrame2 PIXEL FMT: ", scaledFrame2.PixelFormat())

			// Encode the frame
			if err = encodeCodecContext.SendFrame(scaledFrame2); err != nil {
				panic(err)
			}


			// fmt.Println("writeH264ToTrackAR: H2")


			for {
				// fmt.Println("writeH264ToTrackAR: H4")
				// Read encoded packets and write to file
				encodePacket = astiav.AllocPacket()
				if err = encodeCodecContext.ReceivePacket(encodePacket); err != nil {
					if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
						break
					}
					panic(err)
				}

				// fmt.Println("writeH264ToTrackAR: H5")
				// Write H264 to track
				if err = track.WriteSample(media.Sample{Data: encodePacket.Data(), Duration: h264FrameDuration}); err != nil {
					panic(err)
				}
				// fmt.Println("writeH264ToTrackAR: H")
			}
		}
	}
}

func sendFrameToPython(conn net.Conn, frame *astiav.Frame) (*astiav.Frame, error) {	
	// fmt.Println("sendFrameToPython: H1")
	width := frame.Width()
	height := frame.Height()
	fmt.Println("FRAME WIDTH: ", width)
	fmt.Println("FRAME HEIGHT: ", height)

	fmt.Println("FRAME LINESIZE: ")
	lineSize2 := frame.Linesize()
	for i := 0; i < len(lineSize2); i++ {
        fmt.Println(lineSize2[i])
    }

	img := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))

	// fmt.Println("sendFrameToPython: H2")
	frame.Data().ToImage(img)
	// fmt.Println("sendFrameToPython: H3")

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		return frame, err
	}
	// fmt.Println("sendFrameToPython: H4")

    // Send the size of the frame data
    frameSize := uint32(buf.Len())
    if err := binary.Write(conn, binary.BigEndian, frameSize); err != nil {
        return frame, fmt.Errorf("failed to send frame size: %w", err)
    }

	// fmt.Println("sendFrameToPython: H5")
	// Send the frame data
	_, err = conn.Write(buf.Bytes())
	if err != nil {
		return frame, fmt.Errorf("failed to send frame data: %w", err)
	}

	// fmt.Println("sendFrameToPython: H6")
    // Read the size of the processed frame
    var processedFrameSize uint32
    if err := binary.Read(conn, binary.BigEndian, &processedFrameSize); err != nil {
        return frame, fmt.Errorf("failed to read processed frame size: %w", err)
    }

	// fmt.Println("sendFrameToPython: H7")
	// Read the processed frame data
	processedFrameData := make([]byte, processedFrameSize)
	_, err = conn.Read(processedFrameData)
	if err != nil {
		return frame, fmt.Errorf("failed to read processed frame data: %w", err)
	}

	// fmt.Println("sendFrameToPython: H8")
	reader := bytes.NewReader(processedFrameData)

    // Decode the JPEG image from the reader
    processed_img, err := jpeg.Decode(reader)
    if err != nil {
		fmt.Println("DECODING FAILED: ", err)
        return frame, fmt.Errorf("failed to decode image: %w", err)
    }

	rgba_img := image.NewRGBA(processed_img.Bounds())
	draw.Draw(rgba_img, rgba_img.Bounds(), processed_img, processed_img.Bounds().Min, draw.Over)

	fmt.Println("HEIGHT: ", rgba_img.Bounds().Dy())
	fmt.Println("WIDTH: ", rgba_img.Bounds().Dx())
	processedFrame = frame.Clone()
	// processedFrame.SetHeight(processed_img.Bounds().Dy())
	// processedFrame.SetWidth(processed_img.Bounds().Dx())
	// processedFrame.SetPixelFormat(astiav.PixelFormatRgba)
	fmt.Println("AFTER SETTING")
	fmt.Println("HEIGHT: ", processedFrame.Height())
	fmt.Println("WIDTH: ", processedFrame.Width())
	fmt.Println("PIXEL FMT: ", processedFrame.PixelFormat())

	// fmt.Println("sendFrameToPython: H10")
	// align := 0
	// if err := processedFrame.AllocBuffer(align); err != nil {
	// 	return frame, fmt.Errorf("main: allocating buffer failed: %w", err)
	// }
	// fmt.Println("sendFrameToPython: H11")

	// // Alloc image
	// if err := processedFrame.AllocImage(align); err != nil {
	// 	return frame, fmt.Errorf("main: allocating image failed: %w", err)
	// }

	// fmt.Println("sendFrameToPython: H12")
	// if err := processedFrame.MakeWritable(); err != nil {
	// 	return frame, fmt.Errorf("main: making frame writable failed: %w", err)
	// }

	// iswritable := processedFrame.IsWritable()
	// fmt.Println("IS WRITABLE: ", iswritable)

	// // processedFrame.

	fmt.Println("FRAME ISWRITABLE?: ", processedFrame.IsWritable())

	fmt.Println("sendFrameToPython: H12")
	if err := processedFrame.MakeWritable(); err != nil {
		return frame, fmt.Errorf("main: making frame writable failed: %w", err)
	}

	fmt.Println("LINESIZE: ")
	lineSize := processedFrame.Linesize()
	for i := 0; i < len(lineSize); i++ {
        fmt.Println(lineSize[i])
    }

	fmt.Println("sendFrameToPython: H13")
	if err := processedFrame.Data().FromImage(rgba_img);  err != nil {
		return frame, fmt.Errorf("converting processed image to frame failed: %w", err)
	}

	fmt.Println("sendFrameToPython: H14")

	filename := "processed.jpg"
	file, err := os.Create(filename)
    if err != nil {
        return frame, fmt.Errorf("failed to decode image: %w", err)
    }
    defer file.Close()
	fmt.Println("sendFrameToPython: H15")

	err = jpeg.Encode(file, processed_img, nil)
    if err != nil {
        return frame, fmt.Errorf("failed to decode image: %w", err)
    }
	fmt.Println("sendFrameToPython: H16")
	return processedFrame, nil
}


/*
func writeH264ToTrack(track *webrtc.TrackLocalStaticSample) {
	
	This function continuously reads video frames from a specified input, decodes them, 
	scales them, encodes them back into H.264 format, and writes the samples to a WebRTC track.
	
	astiav.RegisterAllDevices()

	initTestSrc()
	initFilters() 
	defer freeVideoCoding()

	ticker := time.NewTicker(h264FrameDuration)
	for ; true; <-ticker.C {
		if err = inputFormatContext.ReadFrame(decodePacket); err != nil {
			if errors.Is(err, astiav.ErrEof) {
				break
			}
			panic(err)
		}
		decodePacket.RescaleTs(videoStream.TimeBase(), decodeCodecContext.TimeBase())

		// Send the packet
		if err = decodeCodecContext.SendPacket(decodePacket); err != nil {
			panic(err)
		}
	
		for {
			// Read Decoded Frame
			if err = decodeCodecContext.ReceiveFrame(decodeFrame); err != nil {
				if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
					fmt.Println("In error block")
					break
				}
				panic(err)
			}

			initVideoEncoding()

			fmt.Println("Pixel format of decoded frame: ", decodeFrame.PixelFormat());

			if err = buffersrcCtx.BuffersrcAddFrame(decodeFrame, astiav.NewBuffersrcFlags(astiav.BuffersrcFlagKeepRef)); err != nil {
				err = fmt.Errorf("main: adding frame failed: %w", err)
				return
			}

			for{
				filterFrame.Unref()

				if err = buffersinkCtx.BuffersinkGetFrame(filterFrame, astiav.NewBuffersinkFlags()); err != nil {
					if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
						break
					}
					panic(err)
				}
	
				pts++
				filterFrame.SetPts(pts)
	
				// Encode the frame
				if err = encodeCodecContext.SendFrame(filterFrame); err != nil {
					panic(err)
				}
	
				for {
					// Read encoded packets and write to file
					encodePacket = astiav.AllocPacket()
					if err = encodeCodecContext.ReceivePacket(encodePacket); err != nil {
						if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
							break
						}
						panic(err)
					}
					// Write H264 to track
					if err = track.WriteSample(media.Sample{Data: encodePacket.Data(), Duration: h264FrameDuration}); err != nil {
						panic(err)
					}
				}
			}
		}
	}
}
*/

func initTestSrc() {
	if inputFormatContext = astiav.AllocFormatContext(); inputFormatContext == nil {
		panic("Failed to AllocCodecContext")
	}

	// Open input
	if err = inputFormatContext.OpenInput("udp://224.0.0.251:5353", nil, nil); err != nil {
		panic(err)
	}

	// Find stream info
	if err = inputFormatContext.FindStreamInfo(nil); err != nil {
		panic(err)
	}

	videoStream = inputFormatContext.Streams()[0]

	// Find decoder
	decodeCodec := astiav.FindDecoder(videoStream.CodecParameters().CodecID())
	if decodeCodec == nil {
		panic("FindDecoder returned nil")
	}

	// Find decoder
	if decodeCodecContext = astiav.AllocCodecContext(decodeCodec); decodeCodecContext == nil {
		panic(err)
	}

	// Update codec context
	if err = videoStream.CodecParameters().ToCodecContext(decodeCodecContext); err != nil {
		panic(err)
	}

	// Set framerate
	decodeCodecContext.SetFramerate(inputFormatContext.GuessFrameRate(videoStream, nil))

	// Open codec context
	if err = decodeCodecContext.Open(decodeCodec, nil); err != nil {
		panic(err)
	}

	fmt.Println("decodeCodecContext.PixelFormat().Name(): ", decodeCodecContext.PixelFormat().Name())


	decodePacket = astiav.AllocPacket()
	decodeFrame = astiav.AllocFrame()
}

func initVideoEncoding() {
	if encodeCodecContext != nil {
		return
	}
	
	// Find encoder
	h264Encoder := astiav.FindEncoder(astiav.CodecIDH264)
	if h264Encoder == nil {
		panic("No H264 Encoder Found")
	}

	// Alloc codec context
	if encodeCodecContext = astiav.AllocCodecContext(h264Encoder); encodeCodecContext == nil {
		panic("Failed to AllocCodecContext Decoder")
	}

	// Update codec context
	encodeCodecContext.SetPixelFormat(astiav.PixelFormatYuv420P)
	encodeCodecContext.SetSampleAspectRatio(decodeCodecContext.SampleAspectRatio())
	encodeCodecContext.SetTimeBase(astiav.NewRational(1, 30))
	encodeCodecContext.SetWidth(decodeCodecContext.Width())
	encodeCodecContext.SetHeight(decodeCodecContext.Height())

	// Open codec context
	if err = encodeCodecContext.Open(h264Encoder, nil); err != nil {
		panic(err)
	}

	softwareScaleContext, err = astiav.CreateSoftwareScaleContext(
		decodeCodecContext.Width(),
		decodeCodecContext.Height(),
		decodeCodecContext.PixelFormat(),
		decodeCodecContext.Width(),
		decodeCodecContext.Height(),
		astiav.PixelFormatRgba,
		astiav.NewSoftwareScaleContextFlags(astiav.SoftwareScaleContextFlagBilinear),
	)

	if err != nil {
		panic(err)
	}

	scaledFrame = astiav.AllocFrame()

	softwareScaleContext2, err = astiav.CreateSoftwareScaleContext(
		softwareScaleContext.DestinationWidth(),
		softwareScaleContext.DestinationHeight(),
		softwareScaleContext.DestinationPixelFormat(),
		softwareScaleContext.SourceWidth(),
		softwareScaleContext.SourceHeight(),
		astiav.PixelFormatYuv420P,
		softwareScaleContext.Flags(),
	)

	if err != nil {
		panic(err)
	}

	scaledFrame2 = astiav.AllocFrame()
}

func initFilters() {
	filterGraph = astiav.AllocFilterGraph()
	if filterGraph == nil {
		panic("filtergraph could not be created")
	}

	// Alloc outputs
	outputs := astiav.AllocFilterInOut()
	if outputs == nil {
		err = errors.New("main: outputs is nil")
		return
	}

	// Alloc inputs
	inputs := astiav.AllocFilterInOut()
	if inputs == nil {
		err = errors.New("main: inputs is nil")
		return
	}

	// Create buffersrc and buffersink filter contexts
	buffersrc := astiav.FindFilterByName("buffer")
	if buffersrc == nil {
		panic("buffersrc is nil")
	}

	buffersink := astiav.FindFilterByName("buffersink")
	if buffersink == nil {
		panic("buffersink is nil")
	}

	fmt.Println("decodeCodecContext.PixelFormat(): ", decodeCodecContext.PixelFormat().Name())
	var err error
	if buffersrcCtx, err = filterGraph.NewFilterContext(buffersrc, "in", astiav.FilterArgs{
		"pix_fmt":      strconv.Itoa(int(decodeCodecContext.PixelFormat())),
		"video_size":   strconv.Itoa(decodeCodecContext.Width()) + "x" + strconv.Itoa(decodeCodecContext.Height()),
		"time_base":    videoStream.TimeBase().String(),
	}); err != nil {
		panic(err)
	}

	if buffersinkCtx, err = filterGraph.NewFilterContext(buffersink, "in", nil); err != nil {
		err = fmt.Errorf("main: creating buffersink context failed: %w", err)
		return
	}

	// Update outputs
	outputs.SetName("in")
	outputs.SetFilterContext(buffersrcCtx)
	outputs.SetPadIdx(0)
	outputs.SetNext(nil)

	// Update inputs
	inputs.SetName("out")
	inputs.SetFilterContext(buffersinkCtx)
	inputs.SetPadIdx(0)
	inputs.SetNext(nil)

	// Link buffersrc and buffersink through the eq filter for brightness
	if err = filterGraph.Parse("eq=brightness=0.5", inputs, outputs); err != nil {
		panic(err)
	}

	if err = filterGraph.Configure(); err != nil {
		err = fmt.Errorf("main: configuring filter failed: %w", err)
		return
	}

	filterFrame = astiav.AllocFrame()
}

func freeVideoCoding() {
	inputFormatContext.CloseInput()
	inputFormatContext.Free()

	decodeCodecContext.Free()
	decodePacket.Free()
	decodeFrame.Free()

	encodeCodecContext.Free()
	encodePacket.Free()
}