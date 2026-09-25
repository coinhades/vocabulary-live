// Leaving DOM is only a visual echo. It must never retain focus, announce old
// values, or accept another action. Product state never waits for this hook.
export function retire(element: Element) {
  hide(element);
  // Cross-fading views coexist briefly. IDs and radio-group names belong only
  // to the incoming live view, not to the outgoing visual echo.
  for (const child of element.querySelectorAll("[id], input[name]")) {
    child.removeAttribute("id");
    if (child instanceof HTMLInputElement) child.removeAttribute("name");
  }
}

export function hide(element: Element) {
  element.setAttribute("aria-hidden", "true");
  if (element instanceof HTMLElement) element.inert = true;
}

// v-show reuses its element on the next tab visit.
export function reveal(element: Element) {
  element.removeAttribute("aria-hidden");
  if (element instanceof HTMLElement) element.inert = false;
}
