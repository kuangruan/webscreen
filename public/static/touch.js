const videoElement = document.getElementById('remoteVideo');
const TOUCH_SAMPLING_RATE = 16; // 采样间隔(ms), 16ms ≈ 60fps

function throttle(func, limit) {
    let inThrottle;
    return function() {
        const args = arguments;
        const context = this;
        if (!inThrottle) {
            func.apply(context, args);
            inThrottle = true;
            setTimeout(() => inThrottle = false, limit);
        }
    }
}

function getCoordinates(clientX, clientY) {
    // Ensure video metadata is loaded
    if (!videoElement.videoWidth || !videoElement.videoHeight) {
        return null;
    }

    const rect = videoElement.getBoundingClientRect();
    const scaleX = videoElement.videoWidth / videoElement.clientWidth;
    const scaleY = videoElement.videoHeight / videoElement.clientHeight;

    const offsetX = clientX - rect.left;
    const offsetY = clientY - rect.top;

    const x = Math.round(offsetX * scaleX);
    const y = Math.round(offsetY * scaleY);

    const clampedX = Math.max(0, Math.min(x, videoElement.videoWidth));
    const clampedY = Math.max(0, Math.min(y, videoElement.videoHeight));

    return { x: clampedX, y: clampedY };
}

// Mouse handling
let isMouseDown = false;
let lastMouseX = 0;
let lastMouseY = 0;

videoElement.addEventListener('mousedown', (event) => {
    if (event.button !== 0) return; // Only Left Click
    isMouseDown = true;
    const coords = getCoordinates(event.clientX, event.clientY);
    if (coords) {
        lastMouseX = coords.x;
        lastMouseY = coords.y;
        sendTouchEvent(TOUCH_ACTION_DOWN, 0, coords.x, coords.y);
    }
});

document.addEventListener('mouseup', (event) => {
    if (isMouseDown) {
        isMouseDown = false;
        let coords = getCoordinates(event.clientX, event.clientY);
        if (!coords) {
             coords = { x: lastMouseX, y: lastMouseY };
        }

        if (coords) {
            sendTouchEvent(TOUCH_ACTION_UP, 0, coords.x, coords.y);
        }
    }
});

const handleMouseMove = throttle((event) => {
    if (event.buttons !== 1) return; // Only when left button is pressed
    const coords = getCoordinates(event.clientX, event.clientY);
    if (coords) {
        lastMouseX = coords.x;
        lastMouseY = coords.y;
        sendTouchEvent(TOUCH_ACTION_MOVE, 0, coords.x, coords.y);
    }
}, TOUCH_SAMPLING_RATE);

videoElement.addEventListener('mousemove', handleMouseMove);


// Multi-touch handling
const activeTouches = new Map(); // identifier -> ptrId

function getPtrId(identifier) {
    if (activeTouches.has(identifier)) {
        return activeTouches.get(identifier);
    }
    // Find a free ID (0-9)
    for (let i = 0; i < 10; i++) {
        let used = false;
        for (let id of activeTouches.values()) {
            if (id === i) {
                used = true;
                break;
            }
        }
        if (!used) {
            activeTouches.set(identifier, i);
            return i;
        }
    }
    return -1;
}

function releasePtrId(identifier) {
    activeTouches.delete(identifier);
}

videoElement.addEventListener('touchstart', (event) => {
    event.preventDefault();
    for (let i = 0; i < event.changedTouches.length; i++) {
        const touch = event.changedTouches[i];
        const ptrId = getPtrId(touch.identifier);
        if (ptrId === -1) continue;

        const coords = getCoordinates(touch.clientX, touch.clientY);
        if (coords) {
            sendTouchEvent(TOUCH_ACTION_DOWN, ptrId, coords.x, coords.y);
        }
    }
}, { passive: false });

videoElement.addEventListener('touchmove', (event) => {
    event.preventDefault();
    for (let i = 0; i < event.changedTouches.length; i++) {
        const touch = event.changedTouches[i];
        if (!activeTouches.has(touch.identifier)) continue;
        const ptrId = activeTouches.get(touch.identifier);

        const coords = getCoordinates(touch.clientX, touch.clientY);
        if (coords) {
            sendTouchEvent(TOUCH_ACTION_MOVE, ptrId, coords.x, coords.y);
        }
    }
}, { passive: false });

videoElement.addEventListener('touchend', (event) => {
    event.preventDefault();
    for (let i = 0; i < event.changedTouches.length; i++) {
        const touch = event.changedTouches[i];
        if (!activeTouches.has(touch.identifier)) continue;
        const ptrId = activeTouches.get(touch.identifier);

        const coords = getCoordinates(touch.clientX, touch.clientY);
        if (coords) {
            sendTouchEvent(TOUCH_ACTION_UP, ptrId, coords.x, coords.y);
        }
        releasePtrId(touch.identifier);
    }
}, { passive: false });

videoElement.addEventListener('touchcancel', (event) => {
    event.preventDefault();
    for (let i = 0; i < event.changedTouches.length; i++) {
        const touch = event.changedTouches[i];
        if (!activeTouches.has(touch.identifier)) continue;
        const ptrId = activeTouches.get(touch.identifier);

        const coords = getCoordinates(touch.clientX, touch.clientY);
        if (coords) {
            sendTouchEvent(TOUCH_ACTION_UP, ptrId, coords.x, coords.y);
        }
        releasePtrId(touch.identifier);
    }
}, { passive: false });

function sendTouchEvent(action, ptrId, x, y) {
    if (!window.ws || window.ws.readyState !== WebSocket.OPEN) {
        console.warn("WebSocket is not open. Cannot send message.");
        return;
    }
    // console.log(`Sending touch event: action=${action}, ptrId=${ptrId}, x=${x}, y=${y}`);
    p = createTouchPacket(action, ptrId, x, y);
    window.ws.send(p);
}