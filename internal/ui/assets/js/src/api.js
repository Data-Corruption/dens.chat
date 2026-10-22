// Calls to the local service. Errors carry the service's message, which is
// written for the member, and the HTTP status.
//
// Every call names the version of Dens that served this page. A service of
// another version, as one an update restarted, refuses it, and the page
// loads again from that service, rather than talking to it in a shape it
// may not know (M9.3).

export class APIError extends Error {
    constructor(message, status) {
        super(message);
        this.status = status;
    }
}

let pageVersion = '';

// setPageVersion names the version that served the page, as it starts.
export function setPageVersion(version) {
    pageVersion = version;
}

// pageHeaders are headers with the page's version added, for any request
// to the service.
export function pageHeaders(headers = {}) {
    return pageVersion ? { ...headers, 'Dens-Page-Version': pageVersion } : headers;
}

// stale says whether the service refused a request as one from another
// version's page.
export function stale(res) {
    return res.status === 409 && !!res.headers.get('Dens-Reload');
}

// reloadIfStale loads the page again where the service refused a request
// as another version's, and says whether it did.
export function reloadIfStale(res) {
    if (!stale(res)) return false;
    window.location.reload();
    return true;
}

async function request(method, path, body) {
    const res = await fetch(path, {
        method,
        headers: pageHeaders(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
        body: body !== undefined ? JSON.stringify(body) : undefined,
        credentials: 'same-origin',
    });
    // The page is loading again; nothing waits for this answer.
    if (reloadIfStale(res)) return new Promise(() => {});
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
