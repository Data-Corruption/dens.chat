// Calls to the local service. Errors carry the service's message, which is
// written for the member, and the HTTP status.

export class APIError extends Error {
    constructor(message, status) {
        super(message);
        this.status = status;
    }
}

async function request(method, path, body) {
    const res = await fetch(path, {
        method,
        headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
        body: body !== undefined ? JSON.stringify(body) : undefined,
        credentials: 'same-origin',
    });
    const text = await res.text();
    let data = null;
    if (text) {
        try {
            data = JSON.parse(text);
        } catch {
            data = text;
        }
    }
    if (!res.ok) {
        let message = `Something went wrong (HTTP ${res.status}).`;
        if (data && typeof data === 'object' && typeof data.error === 'string') message = data.error;
        else if (typeof data === 'string' && data.trim()) message = data.trim();
        const err = new APIError(message, res.status);
        err.data = data;
        throw err;
    }
    return data;
}

export const api = {
    get: (path) => request('GET', path),
    post: (path, body = {}) => request('POST', path, body),
    put: (path, body = {}) => request('PUT', path, body),
    patch: (path, body = {}) => request('PATCH', path, body),
    del: (path) => request('DELETE', path),
};
