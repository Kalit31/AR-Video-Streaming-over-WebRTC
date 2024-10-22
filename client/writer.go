package client

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
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
	/*
	This function continuously reads video frames from a specified input, decodes them, 
	scales them, encodes them back into H.264 format, and writes the samples to a WebRTC track.
	*/
	astiav.RegisterAllDevices()

	initTestSrc()
	initFilters() 
	defer freeVideoCoding()

	conn, err := net.Dial("tcp", "localhost:5005")
    if err != nil {
        panic(err)
    }
	fmt.Println("CONNECTION ESTABLISHED WITH PYTHON SRVR")
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

		fmt.Println("SENDING PACKET")
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

			fmt.Println("RECEIVED DECODED FRAME")

			initVideoEncoding()

			fmt.Println("DECODE FRAME PIXEL FMT: ", decodeFrame.PixelFormat())

			// fmt.Println("INITIALIZED VIDEO ENCODING")

			// // Send the frame to Python for AR filtering
			// filteredFrame, err := sendFrameToPython(decodeFrame)
			// if err != nil {
			// 	panic(err)
			// }
			
			// fmt.Println("writeH264ToTrackAR: H1")

			// pts++
			// filteredFrame.SetPts(pts)

			// Scale the video
			if err = softwareScaleContext.ScaleFrame(decodeFrame, scaledFrame); err != nil {
				panic(err)
			}

			// We don't care about the PTS, but encoder complains if unset
			pts++
			scaledFrame.SetPts(pts)


			fmt.Println("scaledFrame PIXEL FMT: ", scaledFrame.PixelFormat())

			sendFrameToPython(conn, scaledFrame)

			if err = softwareScaleContext2.ScaleFrame(scaledFrame, scaledFrame2); err != nil {
				panic(err)
			}
			

			// We don't care about the PTS, but encoder complains if unset
			pts++
			scaledFrame2.SetPts(pts)
			


			fmt.Println("scaledFrame2 PIXEL FMT: ", scaledFrame2.PixelFormat())


			// Encode the frame
			if err = encodeCodecContext.SendFrame(scaledFrame2); err != nil {
				panic(err)
			}


			fmt.Println("writeH264ToTrackAR: H2")

			// if filteredFrame == nil {
			// 	fmt.Println("writeH264ToTrackAR: filteredFrame is nil")
			// }
			// // Encode the filtered frame directly
			// if err = encodeCodecContext.SendFrame(filteredFrame); err != nil {
			// 	panic(err)
			// }
			// fmt.Println("writeH264ToTrackAR: H3")


			for {
				fmt.Println("writeH264ToTrackAR: H4")
				// Read encoded packets and write to file
				encodePacket = astiav.AllocPacket()
				if err = encodeCodecContext.ReceivePacket(encodePacket); err != nil {
					if errors.Is(err, astiav.ErrEof) || errors.Is(err, astiav.ErrEagain) {
						break
					}
					panic(err)
				}

				fmt.Println("writeH264ToTrackAR: H5")
				// Write H264 to track
				if err = track.WriteSample(media.Sample{Data: encodePacket.Data(), Duration: h264FrameDuration}); err != nil {
					panic(err)
				}
				fmt.Println("writeH264ToTrackAR: H")
			}
		}
	}
}

func sendFrameToPython(conn net.Conn, frame *astiav.Frame) (*astiav.Frame, error) {	
	width := frame.Width()
	height := frame.Height()

	img := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))

	frame.Data().ToImage(img)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		return nil, err
	}

    // Send the size of the frame data
    frameSize := uint32(buf.Len())
    if err := binary.Write(conn, binary.BigEndian, frameSize); err != nil {
        return nil, fmt.Errorf("failed to send frame size: %w", err)
    }

	// Send the frame data
	_, err = conn.Write(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to send frame data: %w", err)
	}

    // Read the size of the processed frame
    var processedFrameSize uint32
    if err := binary.Read(conn, binary.BigEndian, &processedFrameSize); err != nil {
        return nil, fmt.Errorf("failed to read processed frame size: %w", err)
    }

	// Read the processed frame data
	processedFrameData := make([]byte, processedFrameSize)
	_, err = conn.Read(processedFrameData)
	if err != nil {
		return nil, fmt.Errorf("failed to read processed frame data: %w", err)
	}

	reader := bytes.NewReader(processedFrameData)

    // Decode the JPEG image from the reader
    processed_img, err := jpeg.Decode(reader)
    if err != nil {
        return nil, fmt.Errorf("failed to decode image: %w", err)
    }

	filename := "processed.jpg"
	file, err := os.Create(filename)
    if err != nil {
        return nil, fmt.Errorf("failed to decode image: %w", err)
    }
    defer file.Close()

	err = jpeg.Encode(file, processed_img, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to decode image: %w", err)
    }


	// Call the Python script
	// cmd := exec.Command("python3", "/home/kalit/Desktop/GeorgiaTech/Fall_2024/CS_8903/WebRTC_research/ar-filters/app.py")

	// // Set stdin and stdout
	// cmd.Stdin = bytes.NewReader(buf.Bytes())
	// var out bytes.Buffer
	// cmd.Stdout = &out
	// cmd.Stderr = &out

	// err := cmd.Run()
	// if err != nil {
	// 	fmt.Println("ERROR WHILE EXECUTING PYTHON SCRIPT: ", err)
	// 	fmt.Println("OUT: ", out.String())
	// 	return nil, err
	// }

	// // Read the output image (JPEG data) from stdout
	// _, err = jpeg.Decode(&out)
	// if err != nil {
	// 	return nil, err
	// }

	return frame, nil
}

// Convert ASTIAV frame to Go image
func astiavFrameToImage(frame *astiav.Frame) (image.Image, error) {
	fmt.Println("IN astiavFrameToImage")
	// Create an image from the frame's data
	width := frame.Width()
	height := frame.Height()

	// Create a new RGBA image
	rgbaImg := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	fmt.Println("CREATED A TEMPLATE FOR RGB IMAGE")

	// Assuming the frame is in YUV420P format, you need to convert it to RGBA
	// You may need to adapt this based on your actual pixel format
	// yPlane, _ := frame.Data().Bytes(0)
	// uPlane, _ := frame.Data().Bytes(1)
	// vPlane, _ := frame.Data().Bytes(2)
	// a , _ := frame.Data().Bytes(3)
	// b , _ := frame.Data().Bytes(4)

	fmt.Println("astiavFrameToImage: H1")
	frame.Data().ToImage(rgbaImg)
	fmt.Println("astiavFrameToImage: H2")



	// fmt.Println("astiavFrameToImage: H2")
	// fmt.Println("WIDTH: ", width)
	// fmt.Println("height: ", height)
	// fmt.Println("len(yPlane): ", len(yPlane))
	// fmt.Println("len(uPlane): ", len(uPlane))
	// fmt.Println("len(vPlane): ", len(vPlane))
	// fmt.Println("len(a): ", len(a))
	// fmt.Println("len(b): ", len(b))
	
	// for y := 0; y < height; y++ {
	// 	for x := 0; x < width; x += 2 {
	// 		fmt.Println("astiavFrameToImage: H3")
	// 		// Get Y values
	// 		y0 := yPlane[y*width+x]
	// 		y1 := yPlane[y*width+x+1]

	// 		// Get U and V values (downsampled)
	// 		u := uPlane[(y/2)*width/2+x/2]
	// 		v := vPlane[(y/2)*width/2+x/2]

	// 		// Convert YUV to RGBA for first pixel
	// 		r0, g0, b0 := yuvToRGB(y0, u, v)
	// 		rgbaImg.Set(x, y, color.RGBA{r0, g0, b0, 255})

	// 		// Convert YUV to RGBA for second pixel
	// 		r1, g1, b1 := yuvToRGB(y1, u, v)
	// 		rgbaImg.Set(x+1, y, color.RGBA{r1, g1, b1, 255})
	// 		fmt.Println("astiavFrameToImage: H4")
	// 	}
	// }

	return rgbaImg, nil
}


func imageToASTIAVFrame(img image.Image) *astiav.Frame {
	// Get the image dimensions
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	fmt.Println("In imageToASTIAVFrame: width: ", width)
	fmt.Println("In imageToASTIAVFrame: height: ", height)

	// Create a new ASTIAV frame
	frame := astiav.AllocFrame()
	if frame == nil {
		return nil
	}

	// Set the frame dimensions
	frame.SetWidth(width)
	frame.SetHeight(height)
	frame.SetPixelFormat(astiav.PixelFormatYuv420P)

	// Allocate data buffers for Y, U, and V planes
	yPlane := make([]byte, width*height)
	uPlane := make([]byte, (width/2)*(height/2))
	vPlane := make([]byte, (width/2)*(height/2))

	// Fill the YUV planes from the RGBA image
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, _ := img.At(x, y).RGBA()

			// Convert RGB to YUV
			Y := 0.299*float64(r/256) + 0.587*float64(g/256) + 0.114*float64(b/256)
			U := -0.14713*float64(r/256) - 0.28886*float64(g/256) + 0.436*float64(b/256)
			V := 0.615*float64(r/256) - 0.51499*float64(g/256) - 0.10001*float64(b/256)

			// Fill Y plane
			yPlane[y*width+x] = uint8(clamp(Y))

			// Fill U and V planes (downsampling by 2)
			if x%2 == 0 && y%2 == 0 {
				uPlane[(y/2)*(width/2)+(x/2)] = uint8(clamp(U + 128)) // Adjust U
				vPlane[(y/2)*(width/2)+(x/2)] = uint8(clamp(V + 128)) // Adjust V
			}
		}
	}

	// frame.Data().FromImage()
	// frame.AllocBuffer(0)
	

	// Set the planes in the frame
	return frame
}



func yuvToRGB(y uint8, u uint8, v uint8) (uint8, uint8, uint8) {
	Y := float64(y)
	U := float64(u) - 128
	V := float64(v) - 128

	// Convert YUV to RGB
	r := Y + 1.402*V
	g := Y - 0.344136*U - 0.714136*V
	b := Y + 1.772*U

	return uint8(clamp(r)), uint8(clamp(g)), uint8(clamp(b))
}

func clamp(value float64) float64 {
	if value < 0 {
		return 0
	} else if value > 255 {
		return 255
	}
	return value
}


func writeH264ToTrack(track *webrtc.TrackLocalStaticSample) {
	/*
	This function continuously reads video frames from a specified input, decodes them, 
	scales them, encodes them back into H.264 format, and writes the samples to a WebRTC track.
	*/
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