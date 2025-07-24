import {
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from "@/components/ui/form";
import { useFormContext } from "react-hook-form";
import { z } from "zod";
import { useQuery } from "@tanstack/react-query";
import { getTagsOptions } from "@/api/@tanstack/react-query.gen";
import { type TagModel } from "@/api";
import { useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Checkbox } from "@/components/ui/checkbox";
import { X } from "lucide-react";
import { getContrastingTextColor } from "@/lib/utils";

export const tagsDefaultValues = {
  tag_ids: [] as string[],
};

export const tagsSchema = z.object({
  tag_ids: z.array(z.string()),
});

const Tags = () => {
  const form = useFormContext();
  const [tagPopoverOpen, setTagPopoverOpen] = useState(false);

  // Load available tags
  const { data: tagsData } = useQuery({
    ...getTagsOptions({
      query: {
        limit: 100,
      },
    }),
  });

  const availableTags = (tagsData?.data || []) as TagModel[];
  const selectedTagIds = form.watch("tag_ids") || [];

  const handleTagToggle = (tagId: string) => {
    const currentTags = (form.getValues("tag_ids") || []) as string[];
    const newTags = currentTags.includes(tagId)
      ? currentTags.filter((id: string) => id !== tagId)
      : [...currentTags, tagId];
    form.setValue("tag_ids", newTags);
  };

  const handleTagRemove = (tagId: string) => {
    const currentTags = (form.getValues("tag_ids") || []) as string[];
    const newTags = currentTags.filter((id: string) => id !== tagId);
    form.setValue("tag_ids", newTags);
  };

  const selectedTags = availableTags.filter((tag) =>
    selectedTagIds.includes(tag.id!)
  );

  return (
    <FormField
      control={form.control}
      name="tag_ids"
      render={() => (
        <FormItem>
          <FormLabel>Tags</FormLabel>
          <FormControl>
            <div className="space-y-3">
              {/* Selected Tags Display */}
              {selectedTags.length > 0 && (
                <div className="flex flex-wrap gap-2">
                  {selectedTags.map((tag) => (
                    <Badge
                      key={tag.id}
                      variant="secondary"
                      className="flex items-center gap-1"
                      style={{
                        backgroundColor: tag.color,
                        color: getContrastingTextColor(tag.color!),
                      }}
                    >
                      {tag.name}
                      <div
                        role="button"
                        onClick={() => handleTagRemove(tag.id!)}
                      >
                        <X className="h-3 w-3 cursor-pointer" />
                      </div>
                    </Badge>
                  ))}
                </div>
              )}

              {/* Tag Selector */}
              <Popover open={tagPopoverOpen} onOpenChange={setTagPopoverOpen}>
                <PopoverTrigger asChild>
                  <Button variant="outline" className="px-3 font-normal w-full">
                    <span className="text-muted-foreground">Select tags</span>
                  </Button>
                </PopoverTrigger>
                <PopoverContent className="w-auto p-0" align="start">
                  <div className="max-h-60 overflow-y-auto">
                    <div className="">
                      {availableTags.map((tag) => (
                        <div
                          key={tag.id}
                          className="flex items-center space-x-2 p-2 hover:bg-accent hover:text-accent-foreground rounded-sm cursor-pointer"
                          onClick={() => handleTagToggle(tag.id!)}
                        >
                          <Checkbox
                            checked={selectedTagIds.includes(tag.id!)}
                          />
                          <Badge
                            variant="secondary"
                            className="text-xs"
                            style={{
                              backgroundColor: tag.color,
                              color: getContrastingTextColor(tag.color!),
                            }}
                          >
                            {tag.name}
                          </Badge>
                        </div>
                      ))}
                      {availableTags.length === 0 && (
                        <div className="text-center text-muted-foreground text-sm p-4">
                          No tags available
                        </div>
                      )}
                    </div>
                  </div>
                </PopoverContent>
              </Popover>
            </div>
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  );
};

export default Tags;
