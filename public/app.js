const balloonsEl = document.getElementById("balloons");
const confettiCanvas = document.getElementById("confetti");
const composerForm = document.getElementById("composer-form");

const url = new URL(window.location.href);
const queryParams = Object.fromEntries(url.searchParams.entries());

const balloonColors = ["#fbbf24", "#60a5fa", "#f472b6", "#34d399", "#f97316"];

// Composer form handling
if (composerForm) {
    composerForm.addEventListener("submit", async function(e) {
        e.preventDefault();

        const occasion = document.getElementById("occasion-select").value;
        const message = document.getElementById("message-input").value.trim();
        const theme = document.getElementById("theme-select").value;
        const useShortlink = document.getElementById("shortlink-check").checked;
        const button = composerForm.querySelector("button");

        if (!message) {
            document.getElementById("message-input").focus();
            return;
        }

        // Build the full path
        const encodedMessage = message.replace(/ /g, "_");
        let path = "/" + encodedMessage;
        if (occasion) {
            path = "/" + occasion + "/" + encodedMessage;
        }
        if (theme) {
            path += "?theme=" + theme;
        }

        // Direct link or shortlink based on checkbox
        if (!useShortlink) {
            window.location.href = path;
            return;
        }

        // Create shortlink
        button.disabled = true;
        button.textContent = "Criando...";

        try {
            const response = await fetch("/s", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ path: path })
            });

            if (response.ok) {
                const data = await response.json();
                window.location.href = data.short_url;
            } else {
                // Fallback to direct URL on error
                window.location.href = path;
            }
        } catch {
            // Fallback to direct URL on error
            window.location.href = path;
        }
    });
}

function createBalloons() {
    const total = 12;
    for (let i = 0; i < total; i += 1) {
        const balloon = document.createElement("div");
        balloon.className = "balloon";
        const color = balloonColors[i % balloonColors.length];
        balloon.style.background = color;
        balloon.style.left = `${Math.random() * 100}%`;
        balloon.style.animationDelay = `${Math.random() * 6}s`;
        balloon.style.animationDuration = `${6 + Math.random() * 6}s`;
        balloon.style.transform = `translateY(${20 + Math.random() * 20}vh)`;
        balloonsEl.appendChild(balloon);
    }
}

function resizeCanvas() {
    confettiCanvas.width = window.innerWidth;
    confettiCanvas.height = window.innerHeight;
}

const confettiCtx = confettiCanvas.getContext("2d");
const confettiPieces = [];

function createConfetti() {
    const count = 180;
    for (let i = 0; i < count; i += 1) {
        confettiPieces.push({
            x: Math.random() * confettiCanvas.width,
            y: Math.random() * confettiCanvas.height,
            r: Math.random() * 6 + 2,
            d: Math.random() * 8 + 4,
            color: balloonColors[i % balloonColors.length],
            tilt: Math.random() * 10 - 5,
            tiltAngle: 0,
            tiltAngleIncrement: Math.random() * 0.08 + 0.02,
        });
    }
}

function drawConfetti() {
    confettiCtx.clearRect(0, 0, confettiCanvas.width, confettiCanvas.height);
    confettiPieces.forEach((p) => {
        confettiCtx.beginPath();
        confettiCtx.lineWidth = p.r;
        confettiCtx.strokeStyle = p.color;
        confettiCtx.moveTo(p.x + p.tilt + p.r / 2, p.y);
        confettiCtx.lineTo(p.x + p.tilt, p.y + p.tilt + p.r / 2);
        confettiCtx.stroke();
    });
    updateConfetti();
    requestAnimationFrame(drawConfetti);
}

function updateConfetti() {
    confettiPieces.forEach((p) => {
        p.tiltAngle += p.tiltAngleIncrement;
        p.y += (Math.cos(p.d) + 3) / 2;
        p.x += Math.sin(p.d) / 2;
        p.tilt = Math.sin(p.tiltAngle) * 12;

        if (p.y > confettiCanvas.height) {
            p.y = -10;
            p.x = Math.random() * confettiCanvas.width;
        }
    });
}

async function trackView() {
    try {
        const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || null;
        const screenInfo = {
            width: window.screen.width,
            height: window.screen.height,
            devicePixelRatio: window.devicePixelRatio || 1,
        };
        const viewport = {
            width: window.innerWidth,
            height: window.innerHeight,
        };

        await fetch("/api/track", {
            method: "POST",
            headers: {
                "Content-Type": "application/json",
            },
            body: JSON.stringify({
                event: "page_view",
                path: url.pathname,
                query: url.search,
                params: queryParams,
                user_agent: navigator.userAgent,
                referrer: document.referrer || null,
                accept_language: navigator.language || null,
                timezone,
                screen: screenInfo,
                viewport,
                timestamp: new Date().toISOString(),
            }),
            keepalive: true,
        });
    } catch {
        // ignore analytics errors
    }
}

window.addEventListener("resize", resizeCanvas);
resizeCanvas();
createBalloons();
createConfetti();
drawConfetti();
trackView();
