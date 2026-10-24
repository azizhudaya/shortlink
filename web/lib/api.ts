// All calls to the shortlink API live here, so pages only deal with
// already-interpreted results and never with fetch, status codes or JSON.

// Mirrors the API's success shape.
export type CreateResponse = {
  code: string;
  short_url: string;
  long_url: string;
  is_custom: boolean;
  created_at: string;
};

export type Me = { email: string; csrf_token: string };

// Mirrors the API's error shape. Branch on `code`, never on `message` text.
type ErrorResponse = {
  error: { code: string; message: string };
};

export type CreateLinkInput = {
  url: string;
  customAlias?: string;
};

export type ApiResult<T> = { ok: true; data: T } | { ok: false; status: number; message: string };

// The API derives the CSRF token from the session (GET /api/auth/me); every
// mutating request must echo it. Held in memory only, never in storage.
let csrfToken: string | null = null;

export function setCsrfToken(t: string | null) {
  csrfToken = t;
}

async function request<T>(path: string, init: RequestInit = {}): Promise<ApiResult<T>> {
  const headers = new Headers(init.headers);
  const method = (init.method ?? 'GET').toUpperCase();
  if (method !== 'GET' && csrfToken) headers.set('X-CSRF-Token', csrfToken);
  if (init.body !== undefined) headers.set('Content-Type', 'application/json');

  let res: Response;
  try {
    // Same-origin: Caddy proxies /api/* to the API, so no base URL or CORS.
    res = await fetch(path, { ...init, method, headers, credentials: 'same-origin' });
  } catch {
    return { ok: false, status: 0, message: 'Could not reach the server. Check your connection and try again.' };
  }

  if (!res.ok) {
    // A proxy-level failure (e.g. a 502 from Caddy) has no JSON body.
    const body = (await res.json().catch(() => null)) as ErrorResponse | null;
    return {
      ok: false,
      status: res.status,
      message: messageFor(body?.error?.code, body?.error?.message, res.headers.get('Retry-After')),
    };
  }
  if (res.status === 204) return { ok: true, data: null as T };
  return { ok: true, data: (await res.json()) as T };
}

export function me(): Promise<ApiResult<Me>> {
  return request<Me>('/api/auth/me');
}

export async function logout(): Promise<void> {
  await request<null>('/api/auth/logout', { method: 'POST' });
}

export function createLink({ url, customAlias }: CreateLinkInput): Promise<ApiResult<CreateResponse>> {
  return request<CreateResponse>('/api/links', {
    method: 'POST',
    body: JSON.stringify({ url, custom_alias: customAlias || undefined }),
  });
}

// The API's messages are already user-facing, but some cases read better
// with frontend-specific wording.
function messageFor(code: string | undefined, message: string | undefined, retryAfter: string | null): string {
  switch (code) {
    case 'ALIAS_TAKEN':
      return 'That alias is already taken. Try another.';
    case 'BAD_REQUEST':
      return 'Something was wrong with that request. Check the URL and try again.';
    case 'RATE_LIMITED': {
      const minutes = Math.max(1, Math.ceil(Number(retryAfter ?? 60) / 60));
      return `Too many requests. Try again in ${minutes} minute${minutes === 1 ? '' : 's'}.`;
    }
    case 'QUOTA_EXCEEDED':
      return "You've reached today's link limit. New accounts can create 10 links a day for their first week.";
    case 'UNAUTHORIZED':
      return 'Your session has ended. Sign in again.';
    case 'LINK_TAKEN_DOWN':
      return "This link was disabled by the operator and can't be changed.";
    case 'NOT_FOUND':
      return 'That link no longer exists.';
    case 'CSRF_INVALID':
      return 'Your session token is out of date. Reload the page and try again.';
    default:
      // UNSAFE_URL, SAFETY_CHECK_UNAVAILABLE and the rest carry API copy.
      return message ?? 'Something went wrong. Try again.';
  }
}

export type LinkStatus = 'active' | 'disabled' | 'taken_down';
export type LinkItem = CreateResponse & { status: LinkStatus };
export type LinkPage = { links: LinkItem[]; next_cursor: string | null };

export function listLinks(cursor?: string): Promise<ApiResult<LinkPage>> {
  const q = cursor ? `?cursor=${encodeURIComponent(cursor)}` : '';
  return request<LinkPage>(`/api/links${q}`);
}

export function setDisabled(code: string, disabled: boolean): Promise<ApiResult<null>> {
  return request<null>(`/api/links/${encodeURIComponent(code)}`, {
    method: 'PATCH',
    body: JSON.stringify({ disabled }),
  });
}

export function deleteLink(code: string): Promise<ApiResult<null>> {
  return request<null>(`/api/links/${encodeURIComponent(code)}`, { method: 'DELETE' });
}
