const videoElement = document.getElementById('remoteVideo');

// 缓存视频元素的位置和尺寸，避免频繁调用 getBoundingClientRect
let cachedVideoRect = null;
let cachedScaleX = 1;
let cachedScaleY = 1;

// 更新缓存的视频尺寸和位置
function updateVideoCache() {
    if (videoElement.videoWidth && videoElement.videoHeight) {
        cachedVideoRect = videoElement.getBoundingClientRect();
        cachedScaleX = videoElement.videoWidth / videoElement.clientWidth;
        cachedScaleY = videoElement.videoHeight / videoElement.clientHeight;
        return true;
    }
    return false;
}

// 监听视频尺寸变化
videoElement.addEventListener('loadedmetadata', updateVideoCache);
window.addEventListener('resize', updateVideoCache);

// 使用 requestAnimationFrame 批量处理移动事件，减少延迟
let pendingMoveEvents = new Map(); // pointerId -> {x, y}
let rafScheduled = false;

function scheduleMoveSend() {
    if (!rafScheduled && pendingMoveEvents.size > 0) {
        rafScheduled = true;
        requestAnimationFrame(() => {
            rafScheduled = false;
            // 批量发送所有待发送的移动事件
            pendingMoveEvents.forEach((coords, pointerId) => {
                sendTouchEvent(TOUCH_ACTION_MOVE, pointerId, coords.x, coords.y);
            });
            pendingMoveEvents.clear();
        });
    }
}

function getScreenCoordinates(clientX, clientY) {
    // 使用缓存的矩形和缩放比例
    if (!cachedVideoRect) {
        if (!updateVideoCache()) {
            return null;
        }
    }

    const offsetX = clientX - cachedVideoRect.left;
    const offsetY = clientY - cachedVideoRect.top;

    const x = Math.round(offsetX * cachedScaleX);
    const y = Math.round(offsetY * cachedScaleY);

    // Clamp coordinates to be within video bounds
    const clampedX = Math.max(0, Math.min(x, videoElement.videoWidth));
    const clampedY = Math.max(0, Math.min(y, videoElement.videoHeight));

    return { x: clampedX, y: clampedY };
}

// ========== 鼠标事件处理 (单点) ==========
let activeMousePointer = null;

videoElement.addEventListener('mousedown', (event) => {
    if (event.button !== 0) return; // Only Left Click
    activeMousePointer = 0; // 使用 pointerId 0 表示鼠标
    const coords = getScreenCoordinates(event.clientX, event.clientY);
    if (coords) {
        sendTouchEvent(TOUCH_ACTION_DOWN, 0, coords.x, coords.y);
    }
});

videoElement.addEventListener('mouseup', (event) => {
    if (activeMousePointer !== null) {
        const coords = getScreenCoordinates(event.clientX, event.clientY);
        if (coords) {
            sendTouchEvent(TOUCH_ACTION_UP, 0, coords.x, coords.y);
        }
        activeMousePointer = null;
    }
});

videoElement.addEventListener('mousemove', (event) => {
    if (activeMousePointer !== null && event.buttons === 1) {
        const coords = getScreenCoordinates(event.clientX, event.clientY);
        if (coords) {
            pendingMoveEvents.set(0, coords);
            scheduleMoveSend();
        }
    }
});

// 处理鼠标移出视频区域后释放的情况
videoElement.addEventListener('mouseleave', (event) => {
    if (activeMousePointer !== null && event.buttons !== 1) {
        const coords = getScreenCoordinates(event.clientX, event.clientY);
        if (coords) {
            sendTouchEvent(TOUCH_ACTION_UP, 0, coords.x, coords.y);
        }
        activeMousePointer = null;
    }
});

// ========== 触摸事件处理 (多点触控) ==========
const activeTouches = new Map(); // touchIdentifier -> pointerId

videoElement.addEventListener('touchstart', (event) => {
    event.preventDefault();
    updateVideoCache(); // 触摸开始时更新缓存
    
    for (let i = 0; i < event.changedTouches.length; i++) {
        const touch = event.changedTouches[i];
        const pointerId = touch.identifier % 10; // 限制在 0-9 范围内
        activeTouches.set(touch.identifier, pointerId);
        
        const coords = getScreenCoordinates(touch.clientX, touch.clientY);
        if (coords) {
            sendTouchEvent(TOUCH_ACTION_DOWN, pointerId, coords.x, coords.y);
        }
    }
}, { passive: false });

videoElement.addEventListener('touchend', (event) => {
    event.preventDefault();
    
    for (let i = 0; i < event.changedTouches.length; i++) {
        const touch = event.changedTouches[i];
        const pointerId = activeTouches.get(touch.identifier);
        
        if (pointerId !== undefined) {
            const coords = getScreenCoordinates(touch.clientX, touch.clientY);
            if (coords) {
                sendTouchEvent(TOUCH_ACTION_UP, pointerId, coords.x, coords.y);
            }
            activeTouches.delete(touch.identifier);
        }
    }
}, { passive: false });

videoElement.addEventListener('touchmove', (event) => {
    event.preventDefault();
    
    for (let i = 0; i < event.changedTouches.length; i++) {
        const touch = event.changedTouches[i];
        const pointerId = activeTouches.get(touch.identifier);
        
        if (pointerId !== undefined) {
            const coords = getScreenCoordinates(touch.clientX, touch.clientY);
            if (coords) {
                pendingMoveEvents.set(pointerId, coords);
            }
        }
    }
    scheduleMoveSend();
}, { passive: false });

videoElement.addEventListener('touchcancel', (event) => {
    event.preventDefault();
    
    for (let i = 0; i < event.changedTouches.length; i++) {
        const touch = event.changedTouches[i];
        const pointerId = activeTouches.get(touch.identifier);
        
        if (pointerId !== undefined) {
            const coords = getScreenCoordinates(touch.clientX, touch.clientY);
            if (coords) {
                sendTouchEvent(TOUCH_ACTION_UP, pointerId, coords.x, coords.y);
            }
            activeTouches.delete(touch.identifier);
        }
    }
}, { passive: false });

function sendTouchEvent(action, ptrId, x, y) {
    if (!window.ws || window.ws.readyState !== WebSocket.OPEN) {
        console.warn("WebSocket is not open. Cannot send message.");
        return;
    }
    // console.log(`Sending touch event: action=${action}, ptrId=${ptrId}, x=${x}, y=${y}`);
    const p = createTouchPacket(action, ptrId, x, y);
    window.ws.send(p);
}
