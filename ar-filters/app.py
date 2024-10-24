import os
import sys
import cv2
import itertools
import numpy as np
from time import time
import mediapipe as mp
import matplotlib.pyplot as plt
import socket
import struct


mp_face_mesh = mp.solutions.face_mesh
face_mesh_videos = mp_face_mesh.FaceMesh(static_image_mode=False, max_num_faces=1, min_detection_confidence=0.5, min_tracking_confidence=0.3)
eye = cv2.imread('/home/kalit/Desktop/GeorgiaTech/Fall_2024/CS_8903/WebRTC_research/ar-filters/filter_imgs/eye.jpg')
mouth = cv2.imread('/home/kalit/Desktop/GeorgiaTech/Fall_2024/CS_8903/WebRTC_research/ar-filters/filter_imgs/smile.png')

def detectFacialLandmarks(image, face_mesh):
    return face_mesh.process(image[:,:,::-1])

def getSize(image, face_landmarks, INDEXES):
    img_height, img_width, _ = image.shape
    INDEXES_LIST = list(itertools.chain(*INDEXES))
    landmarks = []
    for INDEX in INDEXES_LIST:
        landmarks.append([int(face_landmarks.landmark[INDEX].x*img_width), int(face_landmarks.landmark[INDEX].y*img_height)])
        
    landmarks = np.array(landmarks)
    _, _, width, height = cv2.boundingRect(landmarks)
    return width, height, landmarks

def isOpen(image, face_mesh_results, face_part, threshold=5):
    img_height, img_width, _ = image.shape
    status = {}
    if face_part == "MOUTH":
        INDEXES = mp_face_mesh.FACEMESH_LIPS
    elif face_part == "LEFT EYE":
        INDEXES = mp_face_mesh.FACEMESH_LEFT_EYE
    else:
        INDEXES = mp_face_mesh.FACEMESH_RIGHT_EYE

    for face_no, face_landmarks in enumerate(face_mesh_results.multi_face_landmarks):
        _, height, _ = getSize(image, face_landmarks, INDEXES)
        _, face_height, _ = getSize(image, face_landmarks, mp_face_mesh.FACEMESH_FACE_OVAL)
        if (height/face_height)*100 > threshold:
            status[face_no]='OPEN'
        else:
            status[face_no]='CLOSE'
    
    return status

def overlay(image, filter_img, face_landmarks, face_part, INDEXES):
    try:
        annotated_img = image.copy()
        _, face_part_height, landmarks = getSize(image, face_landmarks, INDEXES)
        filter_img_height, filter_img_width, _  = filter_img.shape
        required_height = int(face_part_height*2)
        resized_filter_img = cv2.resize(filter_img, (int(filter_img_width*(required_height/filter_img_height)), required_height))
        filter_img_height, filter_img_width, _ = resized_filter_img.shape
        _, filter_img_mask = cv2.threshold(cv2.cvtColor(resized_filter_img, cv2.COLOR_BGR2GRAY), 25, 255, cv2.THRESH_BINARY_INV)
        center = landmarks.mean(axis=0).astype('int')
        
        location = (int(center[0]-filter_img_width/2), int(center[1]-filter_img_height/2))
        ROI = image[location[1]: location[1]+filter_img_height, location[0]:location[0]+filter_img_width]
        resultant_img = cv2.bitwise_and(ROI, ROI, mask=filter_img_mask)
        resultant_img = cv2.add(resultant_img, resized_filter_img)
        annotated_img[location[1]: location[1]+filter_img_height, location[0]:location[0]+filter_img_width] = resultant_img
        return annotated_img
    except Exception as e:
        print(f"Failed to overlay filter on top of the image frame: {e}")
        raise e
    
def add_filter_on_frame(frame):
    frame = cv2.flip(frame, 1)
    face_mesh_results = detectFacialLandmarks(frame, face_mesh_videos)
    
    if face_mesh_results.multi_face_landmarks:
        left_eye_status = isOpen(frame, face_mesh_results, 'LEFT EYE', threshold=5)
        right_eye_status = isOpen(frame, face_mesh_results, 'RIGHT EYE', threshold=5)
        mouth_status = isOpen(frame, face_mesh_results, 'MOUTH', threshold=15)
        
        for face_num, face_landmarks in enumerate(face_mesh_results.multi_face_landmarks):
            if left_eye_status[face_num] == 'OPEN':
                frame = overlay(frame, eye, face_landmarks, 'LEFT EYE', mp_face_mesh.FACEMESH_LEFT_EYE)
            if right_eye_status[face_num] == 'OPEN':
                frame = overlay(frame, eye, face_landmarks, 'RIGHT EYE', mp_face_mesh.FACEMESH_RIGHT_EYE)
            if mouth_status[face_num] == 'OPEN':
                frame = overlay(frame, mouth, face_landmarks, 'MOUTH', mp_face_mesh.FACEMESH_LIPS)
    
    return frame

def main():
    server_socket = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    server_socket.bind(('localhost', 5005))
    server_socket.listen(1)
    print("Server is listening for incoming frames...")
    conn, addr = server_socket.accept()
    print(f"Connection established with {addr}")

    while True:
        raw_size = conn.recv(4)
        if not raw_size:
            break
        
        frame_size = struct.unpack('!I', raw_size)[0]
        frame_data = bytearray()
        
        while len(frame_data) < frame_size:
            packet = conn.recv(frame_size - len(frame_data))
            if not packet:
                break
            frame_data.extend(packet)

        nparr = np.frombuffer(frame_data, np.uint8)
        frame = cv2.imdecode(nparr, cv2.IMREAD_UNCHANGED)
        try:
            new_frame = add_filter_on_frame(frame)
        except Exception as e:
            print("Failed to add filter: "+str(e))
            new_frame = frame

        _, buffer = cv2.imencode('.jpg', new_frame)
        output_file_path = 'output.jpg' 
        with open(output_file_path, 'wb') as f:
            f.write(buffer)
        conn.sendall(struct.pack('!I', len(buffer)) + buffer.tobytes())

    conn.close()
    server_socket.close()
        
    
if __name__ == "__main__":
  main()