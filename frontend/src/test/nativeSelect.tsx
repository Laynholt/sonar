import type { ReactNode } from "react";

// jsdom cannot drive the Radix Select popper; tests swap it for a native <select>.
export const Select = ({ value, onValueChange, children }: { value: string; onValueChange: (value: string) => void; children: ReactNode }) =>
    <select value={value} onChange={(event) => onValueChange(event.target.value)}>{children}</select>;
export const SelectItem = ({ value, children }: { value: string; children: ReactNode }) => <option value={value}>{children}</option>;
export const SelectContent = ({ children }: { children: ReactNode }) => <>{children}</>;
export const SelectTrigger = () => null;
export const SelectValue = () => null;
