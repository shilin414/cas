import { useEffect, useLayoutEffect, useRef, type ReactNode } from 'react';

type SwipeAxis = 'pending' | 'horizontal' | 'ignored';

interface SwipeGesture {
  startX: number;
  startY: number;
  startedAt: number;
  axis: SwipeAxis;
}

interface MobileDrawerSwipeSurfaceProps {
  children: ReactNode;
  open: boolean;
  onDismiss: () => void;
}

/** Follow a left swipe without rerendering the navigation list on every move. */
export default function MobileDrawerSwipeSurface({
  children, open, onDismiss,
}: MobileDrawerSwipeSurfaceProps) {
  const surfaceRef = useRef<HTMLDivElement>(null);
  const dismissRef = useRef(onDismiss);
  dismissRef.current = onDismiss;

  // rc-drawer keeps its panel mounted during the exit animation. Preserve the
  // dragged position on close, then clear it before the next opening frame.
  useLayoutEffect(() => {
    if (!open || !surfaceRef.current) return;
    surfaceRef.current.style.transition = '';
    surfaceRef.current.style.transform = '';
  }, [open]);

  useEffect(() => {
    const surface = surfaceRef.current;
    if (!open || !surface) return undefined;

    let gesture: SwipeGesture | null = null;

    const snapBack = () => {
      const reduceMotion = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
      surface.style.transition = reduceMotion
        ? 'none' : 'transform 180ms cubic-bezier(0.2, 0, 0, 1)';
      surface.style.transform = 'translate3d(0, 0, 0)';
    };

    const onTouchStart = (event: TouchEvent) => {
      if (event.touches.length !== 1) return;
      const touch = event.touches[0];
      gesture = {
        startX: touch.clientX,
        startY: touch.clientY,
        startedAt: performance.now(),
        axis: 'pending',
      };
      surface.style.transition = 'none';
    };

    const onTouchMove = (event: TouchEvent) => {
      if (!gesture) return;
      if (event.touches.length !== 1) {
        if (gesture.axis === 'horizontal') snapBack();
        gesture = null;
        return;
      }

      const touch = event.touches[0];
      const dx = touch.clientX - gesture.startX;
      const dy = touch.clientY - gesture.startY;
      if (gesture.axis === 'pending' && Math.max(Math.abs(dx), Math.abs(dy)) >= 10) {
        gesture.axis = dx < 0 && Math.abs(dx) > Math.abs(dy) * 1.2
          ? 'horizontal' : 'ignored';
      }
      if (gesture.axis !== 'horizontal') return;

      if (event.cancelable) event.preventDefault();
      const width = surface.getBoundingClientRect().width;
      const offset = Math.max(-width, Math.min(0, dx));
      surface.style.transform = `translate3d(${offset}px, 0, 0)`;
    };

    const onTouchEnd = (event: TouchEvent) => {
      if (!gesture) return;
      const finished = gesture;
      gesture = null;
      if (finished.axis !== 'horizontal') return;

      const endX = event.changedTouches[0]?.clientX ?? finished.startX;
      const distance = Math.max(0, finished.startX - endX);
      const duration = Math.max(1, performance.now() - finished.startedAt);
      const width = surface.getBoundingClientRect().width;
      const farEnough = distance >= Math.max(64, width * 0.28);
      const quickFlick = distance >= 40 && distance / duration >= 0.55;
      if (farEnough || quickFlick) dismissRef.current();
      else snapBack();
    };

    const onTouchCancel = () => {
      if (gesture?.axis === 'horizontal') snapBack();
      gesture = null;
    };

    surface.addEventListener('touchstart', onTouchStart, { passive: true });
    surface.addEventListener('touchmove', onTouchMove, { passive: false });
    surface.addEventListener('touchend', onTouchEnd);
    surface.addEventListener('touchcancel', onTouchCancel);
    return () => {
      surface.removeEventListener('touchstart', onTouchStart);
      surface.removeEventListener('touchmove', onTouchMove);
      surface.removeEventListener('touchend', onTouchEnd);
      surface.removeEventListener('touchcancel', onTouchCancel);
    };
  }, [open]);

  return <div ref={surfaceRef} className="mobile-shell__swipe-surface">{children}</div>;
}
