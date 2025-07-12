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
import { MultiSelect } from "@/components/multi-select";

export const tagsDefaultValues = {
  tag_ids: [] as string[],
};

export const tagsSchema = z.object({
  tag_ids: z.array(z.string()),
});

const Tags = () => {
  const form = useFormContext();
  const [search, setSearch] = useState("");

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

  // Transform TagModel data to MultiSelect options format
  const tagOptions = availableTags.map((tag) => ({
    label: tag.name || "",
    value: tag.id || "",
    // Note: MultiSelect doesn't support custom colors in the same way,
    // but we could add an icon or handle styling differently if needed
  }));

  const handleValueChange = (newValues: string[]) => {
    form.setValue("tag_ids", newValues);
  };

  return (
    <FormField
      control={form.control}
      name="tag_ids"
      render={() => (
        <FormItem>
          <FormLabel>Tags</FormLabel>
          <FormControl>
            <MultiSelect
              options={tagOptions}
              value={selectedTagIds}
              onValueChange={handleValueChange}
              search={search}
              onSearchChange={setSearch}
              placeholder="Select tags..."
              maxCount={3}
              className="w-full"
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  );
};

export default Tags;
