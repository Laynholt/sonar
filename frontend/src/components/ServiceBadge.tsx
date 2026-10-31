import { Music } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { useSources } from "@/hooks/useSources";
import type { Service } from "@/types/api";
export function ServiceBadge({ service }: {
    service: Service;
}) {
    const { label } = useSources();
    return <Badge variant="outline">{label(service)}</Badge>;
}
/** A source's icon as the plugin gave it (an image address); a source without one gets a generic note. */
export function SourceIcon({ icon }: { icon: string }) {
    return icon ? <img src={icon} alt="" className="h-4 w-4"/> : <Music className="h-4 w-4"/>;
}
