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

	"github.com/asticode/go-astiav"
)


func processImageFrame(conn net.Conn, frame *astiav.Frame) error {
	// Convert frame to RGBA image
	width := frame.Width()
	height := frame.Height()
	img := image.NewRGBA(image.Rect(0, 0, int(width), int(height)))
	frame.Data().ToImage(img)

	// Encode the RGBA image to bugger
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		return err
	}

    // Send the size of the frame data
    frameSize := uint32(buf.Len())
    if err := binary.Write(conn, binary.BigEndian, frameSize); err != nil {
        return  fmt.Errorf("failed to send frame size: %w", err)
    }

	// Send the frame data for processing
	_, err := conn.Write(buf.Bytes())
	if err != nil {
		return fmt.Errorf("failed to send frame data: %w", err)
	}
	return nil
}

func receiveProcessedImageFrame(conn net.Conn) (*image.Image, error) {
    // Read the size of the processed frame
    var processedFrameSize uint32
    if err := binary.Read(conn, binary.BigEndian, &processedFrameSize); err != nil {
        return nil, fmt.Errorf("failed to read processed frame size: %w", err)
    }

	// Read the processed frame data
	processedFrameData := make([]byte, processedFrameSize)
	_, err := conn.Read(processedFrameData)
	if err != nil {
		return nil, fmt.Errorf("failed to read processed frame data: %w", err)
	}

	// Decode the image buffer to image.Image
	reader := bytes.NewReader(processedFrameData)
    processed_img, err := jpeg.Decode(reader)
    if err != nil {
        return nil, fmt.Errorf("failed to decode image: %w", err)
    }
	return &processed_img, nil
}

func convertImageToFrame(img *image.Image, frame *astiav.Frame) (*astiav.Frame, error) {
	imgRGBA := image.NewRGBA((*img).Bounds())
	if imgRGBA == nil {
		return frame, errors.New("Failed to convert image to RGBA format")
	}

	draw.Draw(imgRGBA, imgRGBA.Bounds(), (*img), (*img).Bounds().Min, draw.Over)

	processedFrame := frame.Clone()

	if err := processedFrame.MakeWritable(); err != nil {
		return frame, fmt.Errorf("main: making frame writable failed: %w", err)
	}

	if err := processedFrame.Data().FromImage(imgRGBA);  err != nil {
		return frame, fmt.Errorf("converting processed image to frame failed: %w", err)
	}
	return processedFrame, nil
}

func dumpImageToFile(filename string, img *image.Image) error {
	file, err := os.Create(filename)
    if err != nil {
        return err
    }
    defer file.Close()

	err = jpeg.Encode(file, *img, nil)
    if err != nil {
        return fmt.Errorf("failed to encode image: %w", err)
    }
	return nil
}

func OverlayARFilter(conn net.Conn, frame *astiav.Frame) (*astiav.Frame, error) {	
	if err := processImageFrame(conn, frame); err != nil {
		return frame, err
	}

	processed_image, err := receiveProcessedImageFrame(conn)
	if err != nil {
		return frame, err
	}

	processed_frame, err := convertImageToFrame(processed_image, frame)
	if err != nil {
		return frame, err
	}

	// dumpImageToFile("processed.jpg", processed_image)

	return processed_frame, nil
}
