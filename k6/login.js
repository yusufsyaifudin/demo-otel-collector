import http from "k6/http";
import { check, sleep } from "k6";

// Test configuration
export const options = {
    thresholds: {
        // Assert that 99% of requests finish within 3000ms.
        http_req_duration: ["p(99) < 3000"],
    },
    // Ramp the number of virtual users up and down
    stages: [
        { duration: "5s", target: 10 },
        { duration: "5s", target: 20 },
        { duration: "5s", target: 10 },
        { duration: "5s", target: 15 },
        { duration: "5s", target: 0 },
    ],
};

// Utility to generate random data
function getRandomInt(min, max) {
    return Math.floor(Math.random() * (max - min + 1)) + min;
}

function generateRandomPayload() {
    return JSON.stringify({
        "username": "user1",
        "password": `password${getRandomInt(1, 2)}`,
    });
}

// Simulated user behavior
export default function () {
    // Generate random payload
    const payload = generateRandomPayload();

    let res = http.post("http://192.168.1.34:8082/login", payload, {
        headers: {
            'Content-Type': 'application/json',
        }
    });

    // Validate response status
    check(res, { "status was 200": (r) => r.status === 200 });
    check(res, { "status was 401": (r) => r.status === 401 });
    sleep(1);
}
