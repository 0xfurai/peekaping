"use client";

import { useRef, useState, useCallback, useEffect } from "react";
import { X, Loader2 } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import {
  Command,
  CommandGroup,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { Command as CommandPrimitive } from "cmdk";

export type Option = Record<"value" | "label", string>;

export function SearchableMultiSelect({
  options,
  selected = [],
  onSelect = () => {},
  inputValue = "",
  setInputValue = () => {},
  placeholder = "Select...",
  onLoadMore,
  isLoading = false,
  nextPage = false,
}: {
  options: Option[];
  selected: Option[];
  onSelect: (value: Option[]) => void;
  inputValue: string;
  setInputValue: (value: string) => void;
  placeholder?: string;
  onLoadMore?: () => void;
  isLoading?: boolean;
  nextPage?: boolean;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);

  const handleUnselect = useCallback((option: Option) => {
    const newSelected = selected.filter((s) => s.value !== option.value);
    onSelect(newSelected);
  }, [selected, onSelect]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLDivElement>) => {
      const input = inputRef.current;
      if (input) {
        if (e.key === "Delete" || e.key === "Backspace") {
          if (input.value === "") {
            const newSelected = [...selected];
            newSelected.pop();
            onSelect(newSelected);
          }
        }
        // This is not a default behaviour of the <input /> field
        if (e.key === "Escape") {
          input.blur();
        }
      }
    },
    [inputRef, selected, onSelect]
  );

  const selectables = options.filter(
    (option) => !selected.some((s) => s.value === option.value)
  );

  // IntersectionObserver for infinite scroll
  useEffect(() => {
    if (!sentinelRef.current || !open) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && nextPage && !isLoading && onLoadMore) {
          onLoadMore();
        }
      },
      { threshold: 1.0 }
    );

    observer.observe(sentinelRef.current);

    return () => {
      observer.disconnect();
    };
  }, [nextPage, isLoading, onLoadMore, open]);

  return (
    <Command
      onKeyDown={handleKeyDown}
      className="overflow-visible bg-transparent"
    >
      <div className="group rounded-md border border-input px-3 py-2 text-sm ring-offset-background focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2">
        <div className="flex flex-wrap gap-1">
          {selected.map((option) => {
            return (
              <Badge key={option.value} variant="secondary">
                {option.label}
                <button
                  className="ml-1 rounded-full outline-none ring-offset-background focus:ring-2 focus:ring-ring focus:ring-offset-2"
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      handleUnselect(option);
                    }
                  }}
                  onMouseDown={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                  }}
                  onClick={() => handleUnselect(option)}
                >
                  <X className="h-3 w-3 text-muted-foreground hover:text-foreground" />
                </button>
              </Badge>
            );
          })}
          {/* Avoid having the "Search" Icon */}
          <CommandPrimitive.Input
            ref={inputRef}
            value={inputValue}
            onValueChange={setInputValue}
            onBlur={() => setOpen(false)}
            onFocus={() => setOpen(true)}
            placeholder={placeholder}
            className="ml-2 flex-1 bg-transparent outline-none placeholder:text-muted-foreground"
          />
        </div>
      </div>

      <div className="relative mt-2">
        <CommandList>
          {open && selectables.length > 0 ? (
            <div className="absolute top-0 z-10 w-full rounded-md border bg-popover text-popover-foreground shadow-md outline-none animate-in">
              <CommandGroup 
                className="overflow-auto"
                style={{ maxHeight: "calc(2.25rem * 10.5)" }}
              >
                {selectables.map((option) => {
                  return (
                    <CommandItem
                      key={option.value}
                      onMouseDown={(e) => {
                        e.preventDefault();
                        e.stopPropagation();
                      }}
                      onSelect={() => {
                        setInputValue("");
                        const newSelected = [...selected, option];
                        onSelect(newSelected);
                      }}
                      className={"cursor-pointer"}
                    >
                      {option.label}
                    </CommandItem>
                  );
                })}
                {/* Sentinel element for IntersectionObserver */}
                {nextPage && !isLoading && (
                  <div ref={sentinelRef} style={{ height: 1 }} />
                )}
                {isLoading && (
                  <div className="flex items-center justify-center py-2">
                    <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                  </div>
                )}
              </CommandGroup>
            </div>
          ) : null}
        </CommandList>
      </div>
    </Command>
  );
}
