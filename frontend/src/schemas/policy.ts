import { z } from 'zod';

export const PolicySchema = z.object({
  id: z.number(),
  name: z.string(),
  trafficGB: z.number().nullable().optional(),
  expiryDays: z.number().nullable().optional(),
  limitIp: z.number().nullable().optional(),
  inboundIds: z.array(z.number()).nullable().optional(),
  createdAt: z.number().optional(),
  updatedAt: z.number().optional(),
});

export const PolicyInputSchema = z.object({
  name: z.string().min(1),
  trafficGB: z.number().nullable().optional(),
  expiryDays: z.number().nullable().optional(),
  limitIp: z.number().nullable().optional(),
  inboundIds: z.array(z.number()).nullable().optional(),
});

export const ApplyFieldChangeSchema = z.object({
  email: z.string(),
  field: z.string(),
  oldVal: z.string(),
  newVal: z.string(),
});

export const ApplyPreviewSchema = z.object({
  totalClients: z.number(),
  skipped: z.number(),
  changed: z.number(),
  fieldChanges: z.record(z.string(), z.number()),
  changes: z.array(ApplyFieldChangeSchema),
});

export const ApplyResultSchema = z.object({
  totalClients: z.number(),
  skipped: z.number(),
  changed: z.number(),
  fieldChanges: z.record(z.string(), z.number()),
  inboundsAttached: z.number().optional(),
  inboundsDetached: z.number().optional(),
});

export type Policy = z.infer<typeof PolicySchema>;
export type PolicyInput = z.infer<typeof PolicyInputSchema>;
export type ApplyFieldChange = z.infer<typeof ApplyFieldChangeSchema>;
export type ApplyPreview = z.infer<typeof ApplyPreviewSchema>;
export type ApplyResult = z.infer<typeof ApplyResultSchema>;
