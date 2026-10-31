import type { ReactNode } from "react";
import { Label } from "@/components/ui/label";

// Every control sits in one right-hand column of the same width, so their edges line up.
export function Row({ id, label, description, children }: { id: string; label: string; description?: string; children: ReactNode }) {
    return (<div className="flex items-center justify-between gap-6 px-4 py-4 border-b last:border-b-0">
      <div className="space-y-1 min-w-0">
        <Label htmlFor={id}>{label}</Label>
        {description && <p className="text-xs text-muted-foreground">{description}</p>}
      </div>
      <div className="w-72 shrink-0 flex flex-col items-end [&_[data-slot=select-trigger]]:w-full">{children}</div>
    </div>);
}

export function Block({ title, children }: { title: string; children: ReactNode }) {
    return (<section className="rounded-md border overflow-hidden">
      <h2 className="h-10 px-4 flex items-center border-b bg-muted/50 text-xs font-medium uppercase text-muted-foreground">{title}</h2>
      <div>{children}</div>
    </section>);
}
