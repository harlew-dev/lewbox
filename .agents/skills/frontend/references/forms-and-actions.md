# Forms and actions

Where buttons go, which style they take, in what order, and how forms, dialogs and confirmations behave. **Follow this for every button you add.** If a place matches none of these patterns, ask rather than invent a layout.

**The short version:** actions sit at the **bottom right** of what they act on, the **primary is rightmost**, and only the sign-in pages use full-width buttons.

## Button styles

| Style | For |
| --- | --- |
| **Solid** (dark/neutral) | the primary action of a group. **At most one per group.** |
| **Solid red** (`danger`) | a destructive action, **only** in a danger zone row or as a confirmation's confirm button |
| **Outline** | an alternative that does something (Try again), or a one-off action that isn't the page's main thing (Change password, Sign out other sessions, Download CSV) |
| **Plain / ghost** | Cancel, Back, icon-only buttons |
| **Danger menu item** | a destructive action inside a ⋯ menu (red text and icon) |
| **Text link / text button** | navigation inside text (Forgot password?, Sign up) / an action inside text that doesn't navigate (Use a recovery code) |

- **Sign out is not destructive**: a default menu item after a divider, never red.
- **Cancel** is never primary or red, and is disabled while the action runs.
- Two solid buttons side by side means nobody can tell which is the main one.

## Where the action goes

| Context | Layout | Primary |
| --- | --- | --- |
| **Settings form** (fields in a settings section) | a right-aligned row below the fields; the error notice below that row | rightmost (Save) |
| **One-off settings action** (no fields) | its own right-aligned row at the bottom of the section | the only button (outline) |
| **Dialog** | the dialog's action row, right-aligned. On phones it stacks full width with the **last child on top** | last child: Cancel · **Save changes**; Back · **Next** |
| **Page header** | title left, actions right on the same row, wrapping below on phones (`flex flex-wrap items-end justify-between gap-4`) | rightmost (**New project**) |
| **Record page** (a member, an API key, a project) | name + status badge left, a **⋯ menu** right holding the record's actions (Edit, Change role, Revoke, Remove as danger item); tabs under the heading; **no back link** (the browser's Back) | n/a |
| **Table row** | a ⋯ menu in the last column, right-aligned; never loose buttons | n/a |
| **Danger zone** | a collapsed section **at the bottom** of settings, one row per action: explanation left, red button right (below the text on phones). The zone has no description; each row does. Collapsing is convenience, not a safeguard | the row's button |
| **Action on one field** (the value has a status with something to do) | the status as a badge at the end of the field's label (read with its name); the action as a text button in the field's description, opening a dialog | the dialog's last button |
| **List filters** | see `lists-and-pagination.md#filters-dialog-and-badges` | **Apply filters** |
| **Sign-in pages** (login, register, reset, onboarding) | **one full-width primary** as the form's last control; links (Sign up, Forgot password?) are text links; a required agreement checkbox right above the button | the only button |

Everything signed in and outside a dialog is right-aligned, never left-aligned or full-width.

### Back on sign-in pages

Moving to another page is the browser's Back or a link. A **step on the same page** the browser can't reach (the code step after the password) gets a plain Back button with a left arrow, on the left of the submit row, the submit taking the remaining width; on phones both full width, Back first. Pages with nowhere to go back to have none.

## Behaviour (same everywhere)

### Pending

Run the action through the action hook (`mutations.md`): the button gets `loading={action.loading}` (spinner, `aria-busy`, ignores clicks), the fields are disabled while `loading` (wrap them in a disabled `<fieldset>`), and a double click submits once.

### A disabled Save says why

- A settings Save is **disabled until the value differs from what's saved and would be accepted**: `disabled = clean === null || clean === saved`. "Differs" uses the normalised value: an email differing only in case or spaces is no change.
- A button disabled by form state passes a **`disabledReason`** in one of exactly two wordings:
  - **"No changes made"**: nothing differs from what's saved.
  - **"Fill in required fields"**: something needed is empty or invalid.
- **Only a settings Save is disabled like this.** Every other form — create, sign-in, wizard step — keeps its submit enabled and validates on submit: `aria-invalid` on each invalid field, the message under it, and focus on the first invalid field. Type-to-confirm in a destructive confirmation may stay disabled until the name matches exactly.
- The reason is a tooltip on a wrapper (a disabled button gets no pointer events) and the button's accessible description (`aria-describedby`).
- Not for `loading`, which explains itself.

Why: a silently disabled button is the most common "the app is broken" report.

### Submit vs button

- The primary is **`type="submit"` inside `<form onSubmit>`**, so Enter submits and password managers recognise the form.
- **Every other button in a form is `type="button"`** (make it the component's default), or Cancel, Reveal and Copy submit the form.
- Handle `onSubmit` on the form, never `onClick` on the submit button.

### Errors and success

- A form's failure: the error notice below the action row. A one-off button's failure: an error toast. A field-specific failure: under the field. Success: a toast. (`mutations.md#where-errors-go`)

### Read-only vs disabled fields

| Field state | Use | Why |
| --- | --- | --- |
| The user may see but not change it (an ID, the tenant name for a member) | **read-only** input with a **lock icon at its end**; the reason ("Only owners can rename this"; default "Can’t be changed") is the lock's tooltip and the input's description | stays focusable, selectable and copyable; a screen reader reads the reason with the field |
| Changing it needs more proof than Save (the account email) | editable, with a shield icon ("Protected"); Save then asks for proof in a dialog | nothing is refused, so no lock |
| The form is submitting | **disabled** | nothing should change mid-request |

- Trying to type, delete, paste or drop into a locked field opens the lock's tooltip for 3 s after the last try (Tab, arrows and copy don't count; Escape and blur close it).
- **NEVER `disabled` for "you can't edit this"**: disabled text is low contrast, unselectable and skipped by keyboard and screen readers.
- A reason a field can't be changed goes in the lock, not in a description under the section title.

### Navigation is a link

A button that goes somewhere renders a link (`<Button href>`), never `onClick={() => navigate(...)}`. Links work with middle-click, open-in-new-tab, the status-bar preview and screen readers' link lists. `navigate()` from code is only for "after the action finished".

### Hide what the API would refuse

Show New project only with `project.store`; show the ⋯ menu only if at least one item is permitted. The API still enforces it.

- **A page with no form loses the button, not the fields.** A record page that shows details and edits through a dialog drops its Edit button for a role without `update`; it doesn't show the details as locked fields (locks are for fields that sit in a form the user can otherwise save). Why: a read-only viewer looking at a page of padlocks reads it as broken.
- **Empty states drop their Create call to action for roles that can't create**, and say what will appear instead ("Projects show here once someone creates one."). Why: "Create your first project" with no button, or a button the API refuses, is a dead end.

### Danger zones

The collapsed section at the bottom of settings (table above):

- **Closed = mounted but `hidden`**, not unmounted. Its content loads with the page, so a row's description (e.g. "You're the only owner of Acme", which decides what Delete account does) is right the moment it opens instead of flashing generic text, and each action's dialog state survives. `hidden` keeps the closed buttons out of the tab order and the accessibility tree.
- **NEVER `hidden="until-found"`** (or a library's "hidden until found" option): Ctrl+F would open a danger zone on a text match.
- **Always starts closed**, on every visit and every tenant (key it by the tenant). The open state is never stored, logged or put in the URL.
- The trigger's visible text is **Show** / **Hide** with the section title as a visually hidden suffix, so its accessible name ("Show danger zone") contains the visible word (voice control users say what they see). `aria-expanded` on the trigger.
- The height animation honours `prefers-reduced-motion`.
- It is convenience, not a safeguard: every action inside keeps its own confirmation, type-to-confirm and password. Tests must open it before asserting a button is absent (`testing`).

## Dialogs

- **Reset state on every opening.** Key the dialog's content by an **open counter** that increments each time it opens (`key={openCount}`), so each opening starts empty, without the last opening's values or error. Keying by the target (`"new"`, a record ID) keeps whatever was typed last time: that was the "form doesn't reset after submit" bug.
- **No closing mid-save:** `onClose = () => !action.busy && close()`; Escape and backdrop click do nothing while busy.
- **Focus returns** to whatever was focused when the dialog opened (a dialog opened from a menu item has no opener element of its own: remember `document.activeElement` at open). A menu item closes its menu first and runs its action once focus is back on the menu button.
- **A dialog over a dialog** dims the one beneath again, and Escape closes only the top one. Cancelling the nested one (a "confirm your identity" step) keeps the parent's form intact.
- A dialog with a one-time result (a new secret) warns via a confirmation before closing early (`sensitive-values-and-copying.md`).
- Multi-step dialogs: Back · **Next** on each step; a terminal success or cancelled step has one button (Done / Close) at the bottom right.

## Confirmations

**Every "are you sure?" is one confirmation component.** Never build one by hand from a plain dialog.

**Prefer Undo to a confirmation when the action is frequent and the API can reverse it** (archive, move, delete into a trash): act at once and put **Undo** on the success toast (`mutations.md#every-success-is-a-toast`). Confirm first when the action is irreversible, affects other people, or acts on many items at once. Permanent delete is always a confirmation dialog.

```
<ConfirmationDialog
  open onClose
  title="Delete Acme?"                                           // names the target
  description="This permanently deletes the project. It can’t be undone."
  confirm="Delete project"                                       // verb + object
  variant="danger"                                               // red confirm; "default" otherwise
  disabled={typed !== project.name}  disabledReason="Fill in required fields"
  onConfirm={() => api.projects(t).destroy(project.id)}           // runs through the action hook
  onConfirmed={() => toast.success("Acme deleted.")}
  onError={showOnField}                                           // optional: API error naming a field
>
  {optional fields: type-to-confirm, password}
</ConfirmationDialog>
```

- `onConfirm` runs through the action hook: the button shows `loading`, a double click confirms once, Cancel/Escape/backdrop do nothing until it settles, fields are disabled meanwhile.
- **Failure keeps the dialog open** with the error and the typed fields. **Success closes it, then** calls `onConfirmed`.
- Also used for a dialog with one action and no fields that explains and asks to go ahead ("Didn’t receive the email?" → Cancel · Resend code).
- **Heavy consequences** (deleting a tenant with everyone's data) go in a warning notice at the top of the dialog's fields, with a short description saying what to do.
- **Type-to-confirm:** for irreversible deletion of something with a name. The label quotes the name in code style: Type “`Acme`” to confirm. Confirm stays disabled until it matches exactly. Add the password for the highest-stakes actions (deleting a tenant or the account).
- Labels name what they act on: "Delete project", "Delete workspace", "Delete account". Never a bare "Delete".

### Delete is never a page

Delete is a confirmation dialog opened from a ⋯ menu item or a danger zone row, calling the client's `destroy`. There is no `/projects/{id}/delete` page (see `naming`). After deleting from the record's own page, navigate to its list. Deleting into a trash is not this dialog: it acts at once with Undo on the toast.

## Examples

```
// Settings form: fields, Save bottom right, error below.
<form onSubmit={submit} class="space-y-4">
  <NameField value={name} onChange={setName} disabled={rename.loading} />
  <div class="flex justify-end">
    <Button type="submit" loading={rename.loading}
            disabled={clean === null || clean === tenant.name}
            disabledReason={clean === null ? "Fill in required fields" : "No changes made"}>
      Save
    </Button>
  </div>
  <ErrorNotice error={rename.error} />   // renders nothing until rename fails
</form>

// One-off settings action.
<div class="flex justify-end">
  <Button outline loading={revoke.loading} onClick={revokeOthers}>Sign out other sessions</Button>
</div>

// Sign-in page.
<Button type="submit" class="w-full" loading={login.loading}>Sign in</Button>
```

## Check your change

Look at it at phone width and desktop width. Every action is at the bottom right of what it acts on (or full width on a sign-in page), the primary rightmost (on top when stacked).

## Before you finish

| Pattern | Fix |
| --- | --- |
| A settings Save that is always enabled | Disabled until the normalised value differs and is valid |
| A disabled Save with no `disabledReason` | "No changes made" or "Fill in required fields" |
| A create, sign-in or wizard submit disabled until valid | Keep it enabled and validate on submit |
| Validation shown only in a toast or at the top of the form | Under each field, `aria-invalid`, focus the first invalid one |
| `onClick` on the submit button | `onSubmit` on the form |
| A Cancel, Reveal or Copy button without `type="button"` | Add it |
| A confirmation built from `Dialog` by hand | The one confirmation component |
| A confirmation dialog for archiving, moving or trashing | Act at once, Undo on the success toast |
| An irreversible, shared or bulk action with no confirmation | Confirm first |
| A `/…/delete` route or a delete page | A confirmation dialog |
| Type-to-confirm whose confirm is enabled on any input | Disabled until the name matches exactly |
| `disabled` on a value the user may see but not change | Read-only with a lock and the reason |
| Signed-in actions left-aligned or full width | Bottom right of what they act on |
| Two solid buttons in one group | One primary; the other outline or plain |
| Red on a non-destructive action | Red only in a danger zone, a confirmation, or a danger menu item |
| A dialog keyed by its target | Key by an open counter |
| A dialog that can close while `busy` | `onClose = () => !action.busy && close()` |
| A dialog that starts open state from the last opening | Reset on every opening |
