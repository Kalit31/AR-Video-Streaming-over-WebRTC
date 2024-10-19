import cv2
import itertools
import numpy as np
from time import time
import mediapipe as mp
import matplotlib.pyplot as plt


mp_face_mesh = mp.solutions.face_mesh

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
    
def main():
    camera_video = cv2.VideoCapture(0)
    camera_video.set(3, 1200)
    camera_video.set(4, 960)
    
    cv2.namedWindow('Face Filter', cv2.WINDOW_NORMAL)
    eye = cv2.imread('ar-filters/eye.jpg')
    mouth = cv2.imread('ar-filters/smile.png')
    
    face_mesh_videos = mp_face_mesh.FaceMesh(static_image_mode=False, max_num_faces=1, min_detection_confidence=0.5, min_tracking_confidence=0.3)

    while camera_video.isOpened():
        ok, frame = camera_video.read()
        if not ok:
            continue
        frame = cv2.flip(frame, 1)
        face_mesh_results = detectFacialLandmarks(frame, face_mesh_videos)
        
        if face_mesh_results.multi_face_landmarks:
            left_eye_status = isOpen(frame, face_mesh_results, 'LEFT EYE', threshold=6)
            right_eye_status = isOpen(frame, face_mesh_results, 'RIGHT EYE', threshold=6)
            mouth_status = isOpen(frame, face_mesh_results, 'MOUTH', threshold=15)
            
            # print(f"mouth_status: {mouth_status}")
            # print(f"left_eye_status: {left_eye_status}")
            # print(f"right_eye_status: {right_eye_status}")
            
            for face_num, face_landmarks in enumerate(face_mesh_results.multi_face_landmarks):
                if left_eye_status[face_num] == 'OPEN':
                    frame = overlay(frame, eye, face_landmarks, 'LEFT EYE', mp_face_mesh.FACEMESH_LEFT_EYE)
                if right_eye_status[face_num] == 'OPEN':
                    frame = overlay(frame, eye, face_landmarks, 'RIGHT EYE', mp_face_mesh.FACEMESH_RIGHT_EYE)
                if mouth_status[face_num] == 'OPEN':
                    frame = overlay(frame, mouth, face_landmarks, 'MOUTH', mp_face_mesh.FACEMESH_LIPS)
    
        cv2.imshow('Face Filter', frame)
        k = cv2.waitKey(1) & 0xFF
        
        if(k==27):
            break
    
    camera_video.release()
    cv2.destroyAllWindows()
                    
        
    
if __name__ == "__main__":
  main()