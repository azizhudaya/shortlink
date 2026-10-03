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

// Mirrors the API's error shape. Branch on `code`, never on `message` text.
type ErrorResponse = {
  error: { code: string; message: string };
};

export type CreateLinkInput = {
  url: string;
  customAlias?: string;
};

export type ApiResult<T> = { ok: true; data: T } | { ok: false; message: string };

export async function createLink({ url, customAlias }: CreateLinkInput): Promise<ApiResult<CreateResponse>> {
  let res: Response;
  try {
    // Same-origin: Caddy proxies /api/* to the API, so no base URL or CORS.
    res = await fetch('/api/links', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        url,
        custom_alias: customAlias || undefined,
      }),
    });
  } catch {
    return { ok: false, message: 'Could not reach the server. Check your connection and try again.' };
  }

  if (!res.ok) {
    // A proxy-level failure (e.g. a 502 from Caddy) has no JSON body.
    const body = (await res.json().catch(() => null)) as ErrorResponse | null;
    return { ok: false, message: messageFor(body?.error?.code, body?.error?.message) };
  }

  return { ok: true, data: (await res.json()) as CreateResponse };
}

// The API's messages are already user-facing, but a couple of cases read
// better with frontend-specific wording.
function messageFor(code: string | undefined, message: string | undefined): string {
  switch (code) {
    case 'ALIAS_TAKEN':
      return 'That alias is already taken. Try another.';
    case 'BAD_REQUEST':
      return 'Something was wrong with that request. Check the URL and try again.';
    default:
      return message ?? 'Could not create the link.';
  }
}
