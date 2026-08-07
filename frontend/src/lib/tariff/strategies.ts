export const TrafficResolutionStrategy = {
  Overwrite: 'overwrite',
  Sum: 'sum',
  Union: 'union',
} as const;

export type TrafficResolutionStrategy = (typeof TrafficResolutionStrategy)[keyof typeof TrafficResolutionStrategy];
