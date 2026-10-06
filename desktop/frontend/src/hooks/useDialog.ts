import { useEffect, useRef } from "react";

// A dialog owns the keyboard while it is up. Without this, Tab from the
// button that opened one walks straight into the page behind it: the sheet
// is on screen, the focus ring is somewhere underneath, and a keyboard user
// is editing a form they cannot see. The hook puts focus inside on open,
// keeps Tab in a loop within the sheet, and hands focus back to whatever had
// it when the sheet closes.
//
// Returns the ref to attach to the sheet element (the panel, not the
// backdrop). The caller still supplies role="dialog" and aria-modal.
const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function useDialog<T extends HTMLElement>() {
  const ref = useRef<T | null>(null);
  useEffect(() => {
    const sheet = ref.current;
    if (!sheet) return;
    const before = document.activeElement as HTMLElement | null;
    const inside = () => Array.from(sheet.querySelectorAll<HTMLElement>(FOCUSABLE)).filter((el) => el.offsetParent !== null);

    // something in the sheet, or the sheet itself, so the next Tab starts here
    const first = inside()[0];
    if (first) first.focus();
    else {
      sheet.tabIndex = -1;
      sheet.focus();
    }

    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Tab") return;
      const items = inside();
      if (items.length === 0) return;
      const top = items[0];
      const last = items[items.length - 1];
      const here = document.activeElement;
      if (!sheet.contains(here)) {
        e.preventDefault();
        (e.shiftKey ? last : top).focus();
        return;
      }
      if (e.shiftKey && here === top) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && here === last) {
        e.preventDefault();
        top.focus();
      }
    };
    document.addEventListener("keydown", onKey, true);
    return () => {
      document.removeEventListener("keydown", onKey, true);
      before?.focus?.();
    };
  }, []);
  return ref;
}
