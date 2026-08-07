import { z } from 'zod';

export const ProfileSchema = z.object({
  id: z.number(),
  name: z.string(),
  traffic: z.number().int().nonnegative().nullable(),
  expiryDays: z.number().int().nonnegative().nullable(),
  limitIp: z.number().int().nonnegative().nullable(),
  inboundIds: z.array(z.number()),
  tariffCount: z.number().int().nonnegative(),
  createdAt: z.number().optional(),
  updatedAt: z.number().optional(),
});

export const ProfileFormSchema = z.object({
  name: z.string().trim().min(1),
  traffic: z.number().int().nonnegative().nullable(),
  expiryDays: z.number().int().nonnegative().nullable(),
  limitIp: z.number().int().nonnegative().nullable(),
  inboundIds: z.array(z.number()),
});

export type Profile = z.infer<typeof ProfileSchema>;
export type ProfileFormValues = z.infer<typeof ProfileFormSchema>;
