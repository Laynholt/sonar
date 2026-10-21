import { Badge } from "@/components/ui/badge";
import type { Service } from "@/types/api";
export const serviceLabels: Record<Service, string> = { spotify: "Spotify", amazon: "Amazon Music" };
export function ServiceBadge({ service }: {
    service: Service;
}) {
    return <Badge variant="outline">{serviceLabels[service]}</Badge>;
}
