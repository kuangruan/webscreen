
const videoElementScroll = document.getElementById('remoteVideo');
const SCROLL_SCALE = 30; // 调整此值以改变滚动灵敏度

// 使用 requestAnimationFrame 批量处理滚动事件
let pendingScroll = { x: 0, y: 0, hScroll: 0, vScroll: 0 };
let scrollRafScheduled = false;
let lastScrollCoords = null;

function sendPendingScroll() {
    if (pendingScroll.hScroll !== 0 || pendingScroll.vScroll !== 0) {
        const packet = createScrollPacket(
            pendingScroll.x, 
            pendingScroll.y, 
            pendingScroll.hScroll, 
            pendingScroll.vScroll
        );
        
        if (window.ws && window.ws.readyState === WebSocket.OPEN) {
            window.ws.send(packet);
        }
        
        // 重置累积的滚动量
        pendingScroll.hScroll = 0;
        pendingScroll.vScroll = 0;
    }
}

const handleWheel = (event) => {
    // 阻止默认的页面滚动行为
    event.preventDefault();

    const coords = getScreenCoordinates(event.clientX, event.clientY);
    if (!coords) return;

    // 缓存坐标位置，用于后续批量发送
    lastScrollCoords = coords;
    
    // 增加滚动敏感度

    // 累积滚动量
    pendingScroll.x = coords.x;
    pendingScroll.y = coords.y;
    pendingScroll.hScroll += Math.round(event.deltaX * SCROLL_SCALE);
    pendingScroll.vScroll += -Math.round(event.deltaY * SCROLL_SCALE);

    // 使用 requestAnimationFrame 批量发送，减少消息数量
    if (!scrollRafScheduled) {
        scrollRafScheduled = true;
        requestAnimationFrame(() => {
            scrollRafScheduled = false;
            sendPendingScroll();
        });
    }
};

videoElementScroll.addEventListener('wheel', handleWheel, { passive: false });
