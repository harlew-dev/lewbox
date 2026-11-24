# Mutations

Every create, update, delete and named action (accept, resend, sign out others) runs through **one action hook**. No hand-written `try/catch/finally` with a `saving` boolean in components: each copy gets double-submit, the spinner flash or the error reset slightly wrong.

## The action hook

```
save = useAction(async (input) => {
  project = await api.projects(tenant.id).store(input)
  revalidateProjects()                       // data-loading.md
  toast.success("Project created.")
  await navigate(routes.project.show(tenant.id, project.id))
  return project
}, { onError? })

result = await save.run(form)   // { ok: true, value } | { ok: false }; NEVER throws
save.busy     // true from run() until settled
save.loading  // true once busy has lasted 200 ms, then held >= 400 ms
save.error    // the last failure (message, field, status, requestId), or null
```

| Flag | True | Use it for |
| --- | --- | --- |
| `busy` | immediately, until the action settles | **logic**: refuse to close the dialog mid-save (`onClose = () => !save.busy && close()`), disable Cancel |
| `loading` | after 200 ms of `busy`, then for at least 400 ms | **what the user sees**: the button's spinner, disabled fields |

Why two flags: a 50 ms request that greys out the form and flashes a spinner looks broken; a spinner that appears for 30 ms looks like a glitch. Double submits are refused by `run` regardless, from the first millisecond.

### Rules the hook enforces

- **Re-entry is ignored.** A second `run` while one is in flight returns without calling the API. Guard with a ref/non-reactive flag, not state (state updates are async; a fast double click sees the old value). The ref survives a dev remount (`dev-double-render.md`).
- **It awaits everything the action awaits, including navigation.** An action that ends with `navigate(...)` stays busy and loading until the next page has rendered, so the button doesn't stop spinning and invite a second click while the router is still loading.
- **It never throws.** The result union forces the caller to branch (`if (result.ok) close()`); a forgotten `await` can't produce an unhandled rejection.
- **The error is announced again if it repeats.** Submitting twice with the same wrong password must re-announce "Incorrect password." (re-key the alert, or clear then set; `ErrorNotice` does this once for every form, `design-system.md#components`).
- **`onError(e)` returning `true`** means the caller handled it (a toast, a field error); `error` stays null.
- A successful `run` clears the previous error.
- **An error shows at once**, even while the spinner is still held for its 400 ms minimum. Holding the error back to match the spinner only delays the answer.
- **Fields stay editable during the first 200 ms** (until `loading`); an edit made then isn't in the request already sent. This is an accepted trade-off for not greying out fast saves; a guard that must hold from the first millisecond (no closing the dialog, no second submit) reads `busy`, never `loading`.

## Where errors go

| Situation | Shown as |
| --- | --- |
| A form (fields + submit) | an error notice (`role="alert"`) **below the action row**, the fields still filled in |
| The API names a field (`error.field === "currentPassword"`) | the message **under that field** (`aria-invalid`, `aria-describedby`); clear the field if it's a code or password |
| A button with no fields (Sign out other sessions, Acknowledge) | an **error toast** (`toast.error(message)`) |
| Inside a dialog (including confirmations) | **in the dialog**, which stays open with everything typed |
| A failed load | the page's notice with Retry, beside the kept data (`data-loading.md`) |
| 401 | navigate to login with `?next=` |
| 429 | the API's message ("Try again in 12 minutes."), no auto-retry |
| 428 challenge (CAPTCHA) | handled centrally by the API client: it opens one challenge dialog, resends the request once with the token; `busy` stays true meanwhile; Cancel becomes an ordinary error ("Check cancelled.") with the form kept. Pages never handle it. |
| Timeout / network | "Couldn’t reach the server. Check your connection and try again." in the same place as other errors |

Show the API's message: the API writes human messages for 4xx. A 5xx has a generic message plus its request ID (below).

## Every success is a toast

- Every finished action says so: created, renamed, deleted, invited, role changed, removed, revoked, password changed, sessions signed out. `toast.success("Workspace renamed.")`.
- Not a `done` state or an inline green banner: those need clearing, move the layout, and are missed when the user's eyes are on the button.
- **Exceptions:** the action's result is itself the confirmation (signing out lands on login; a dialog that switches to a "success" step, such as a one-time secret).
- **Leaving the toaster's layout:** a toast fired just before navigating outside the layout that mounts the toaster (to onboarding, to login) is unmounted with it. Pass the message in navigation state and let the next page show it ("Your account has been deleted." on login). Arriving into the layout works the same way: the layout reads `{ createdWorkspace: "Acme" }` and toasts "Acme created.".
- Pages without a toaster (sign-in pages) show an inline success notice instead.

## The request ID

- A 5xx response body carries the request's ID (`{ "message": "Something went wrong.", "requestId": "req_…" }`). The API client exposes it as `error.requestId`, reading it **only in the shape the API sends** (never a header the client invented, never a value from the URL).
- The error notice shows **"Reference: req_…"** under the message, selectable as a whole (`user-select: all`), and the "service unavailable" page shows it too.
- Why: the user can quote it to support, and the server's log lines are found by it (see `production-hardening`). Without it, "I got an error at about 3pm" is unsearchable.

## Request hygiene

- **CSRF is automatic.** The API client reads the token from the session context and sends it on mutations; components never handle it. Never put it in a URL or `localStorage` (see `security-defaults`).
- **Bodies carry only the editable fields**, picked by the client method from the input: the API rejects unknown fields, and sending the whole record risks mass assignment.
- **Never auto-retry a mutation.** A timed-out `POST` may have succeeded; retrying can create two records or issue two keys. Retry is the user's explicit click.
- **Permissions:** hide what the API would refuse (`can(tenant, "project.store")` → no New project button), but the API stays the authority; handle its 403 like any error.

## Example: a dialog form

```
create = useAction(input => api.projects(tenant.id).store(input), {})

onSubmit(event):
  event.preventDefault()
  result = await create.run(form)
  if result.ok: toast.success("Project created."); close()

<Dialog open onClose={() => !create.busy && close()}>
  <form onSubmit>
    <Fieldset disabled={create.loading}> …fields… </Fieldset>
    <ErrorNotice error={create.error} />   // binds the error to Notice variant="error" (design-system.md)
    <DialogActions>
      <Button plain onClick={close} disabled={create.busy}>Cancel</Button>
      <Button type="submit" loading={create.loading}>Create project</Button>
    </DialogActions>
  </form>
</Dialog>
```

**Common mistakes:** `onClick` on the submit button instead of `onSubmit` on the form (Enter stops working); `disabled={saving}` on the submit button instead of `loading` (no spinner, no accessible busy state); setting `saving=false` before the navigation finishes.
