"use client";

import { useState } from "react";
import { Eye, EyeOff, Copy, Check, Loader2, CheckCircle2, AlertCircle } from "lucide-react";

export interface CredentialFieldProps {
  id: string;
  value: string;
  onChange: (val: string) => void;
  placeholder?: string;
  connected?: boolean;
  busy?: boolean;
  onSubmit: () => void;
  submitLabel?: string;
  statusMessage?: string;
  isError?: boolean;
}

export function CredentialField({
  id,
  value,
  onChange,
  placeholder,
  connected = false,
  busy = false,
  onSubmit,
  submitLabel = "Connect",
  statusMessage,
  isError = false,
}: CredentialFieldProps) {
  const [showPassword, setShowPassword] = useState(false);
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    if (!value) return;
    navigator.clipboard.writeText(value);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="flex flex-col gap-2 w-full">
      <div className="flex flex-col sm:flex-row gap-2">
        <div className="relative flex-1">
          <input
            id={id}
            type={showPassword ? "text" : "password"}
            value={value}
            onChange={(e) => onChange(e.target.value)}
            placeholder={connected ? "•••••••• (paste to replace)" : placeholder}
            className="w-full field text-sm pr-16"
          />
          <div className="absolute right-2 top-1/2 -translate-y-1/2 flex items-center gap-1">
            {value && (
              <button
                type="button"
                onClick={handleCopy}
                className="p-1 text-zinc-400 hover:text-white transition-colors"
                title="Copy token"
              >
                {copied ? <Check className="w-3.5 h-3.5 text-green-400" /> : <Copy className="w-3.5 h-3.5" />}
              </button>
            )}
            <button
              type="button"
              onClick={() => setShowPassword((v) => !v)}
              className="p-1 text-zinc-400 hover:text-white transition-colors"
              title={showPassword ? "Hide token" : "Show token"}
            >
              {showPassword ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
            </button>
          </div>
        </div>

        <button
          type="button"
          onClick={onSubmit}
          disabled={busy || !value.trim()}
          className="flex items-center justify-center gap-2 btn-primary px-4 py-2 rounded-lg font-semibold disabled:opacity-50 shrink-0 transition-colors"
        >
          {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
          {connected ? "Update" : submitLabel}
        </button>
      </div>

      {statusMessage && (
        <div className={`flex items-center gap-2 text-xs mt-1 ${isError ? "text-red-400" : "text-green-400"}`}>
          {isError ? <AlertCircle className="w-3.5 h-3.5 shrink-0" /> : <CheckCircle2 className="w-3.5 h-3.5 shrink-0" />}
          <span>{statusMessage}</span>
        </div>
      )}
    </div>
  );
}
