import { CircleCheckIcon, InfoIcon, Loader2Icon, OctagonXIcon, TriangleAlertIcon, } from "lucide-react";
import { Toaster as Sonner, type ToasterProps } from "sonner";
const Toaster = ({ ...props }: ToasterProps) => {
    return (<Sonner theme="system" className="toaster group" icons={{
            success: <CircleCheckIcon className="size-4"/>,
            info: <InfoIcon className="size-4"/>,
            warning: <TriangleAlertIcon className="size-4"/>,
            error: <OctagonXIcon className="size-4"/>,
            loading: <Loader2Icon className="size-4 animate-spin"/>,
        }} toastOptions={{
            classNames: {
                success: "border-green-500 bg-green-50 text-green-900 dark:bg-green-950 dark:text-green-100 [&>svg]:text-green-500",
                error: "border-red-500 bg-red-50 text-red-900 dark:bg-red-950 dark:text-red-100 [&>svg]:text-red-500",
                warning: "border-yellow-500 bg-yellow-50 text-yellow-900 dark:bg-yellow-950 dark:text-yellow-100 [&>svg]:text-yellow-500",
                info: "border-blue-500 bg-blue-50 text-blue-900 dark:bg-blue-950 dark:text-blue-100 [&>svg]:text-blue-500",
            },
        }} style={{
            "--normal-bg": "var(--popover)",
            "--normal-text": "var(--popover-foreground)",
            "--normal-border": "var(--border)",
            "--border-radius": "var(--radius)",
        } as React.CSSProperties} {...props}/>);
};
export { Toaster };
