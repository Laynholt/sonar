import type { PointerEvent, Ref } from "react";
import { Check } from "lucide-react";
import { useI18n } from "@/lib/i18n";

const RADIUS = 26, LENGTH = 2 * Math.PI * RADIUS;

interface Props {
    ref: Ref<HTMLButtonElement>;
    progress: number;
    speed: string;
    done: boolean;
    attention: boolean;
    onExpand: () => void;
    onPointerDown: (e: PointerEvent<HTMLButtonElement>) => void;
    onPointerMove: (e: PointerEvent<HTMLButtonElement>) => void;
    onPointerUp: (e: PointerEvent<HTMLButtonElement>) => void;
    onPointerCancel: (e: PointerEvent<HTMLButtonElement>) => void;
    className: string;
}

/** The collapsed download widget: a round progress ring with the speed, or a check once finished. */
export function DownloadBadge({ progress, speed, done, attention, onExpand, className, ...rest }: Props) {
    const { t } = useI18n();
    return (<button type="button" {...rest} aria-label={t("widget.badgeLabel", { percent: Math.round(progress * 100), speed })}
        className={`${className} h-[60px] w-[60px] rounded-full bg-neutral-900 text-white shadow-lg cursor-grab active:cursor-grabbing select-none touch-none`}
        onClick={(e) => { if (e.detail === 0) onExpand(); }}>
      <svg viewBox="0 0 60 60" className="absolute inset-0 -rotate-90" aria-hidden>
        <circle cx="30" cy="30" r={RADIUS} fill="none" stroke="#444441" strokeWidth="4"/>
        <circle cx="30" cy="30" r={RADIUS} fill="none" stroke="#5DCAA5" strokeWidth="4" strokeLinecap="round"
            strokeDasharray={LENGTH} strokeDashoffset={LENGTH * (1 - Math.min(1, progress))}/>
      </svg>
      {done
        ? <Check data-testid="badge-done" className="relative mx-auto h-5 w-5 text-[#5DCAA5]"/>
        : <span className="relative flex flex-col items-center text-[11px] leading-tight tabular-nums"><span>{speed}</span><span className="text-[9px] text-neutral-400">{t("widget.badgeSpeedUnit")}</span></span>}
      {attention && <span data-testid="badge-attention" className="absolute right-0 top-0 h-3 w-3 rounded-full bg-amber-500 ring-2 ring-neutral-900"/>}
    </button>);
}
