import { describe, expect, it } from 'vitest';
import { buildAIFixURL, isAIFixURLValid } from './reportUtils';

describe('AI fix URL helpers', () => {
  const validURL = 'https://starmind-dev.sicarrier.com/shield-fix/fixdefect?id={defect_id}';

  it('accepts an HTTPS URL with exactly one defect id placeholder', () => {
    expect(isAIFixURLValid(validURL)).toBe(true);
  });

  it('rejects missing, non-HTTPS, and duplicate placeholder URLs', () => {
    expect(isAIFixURLValid('')).toBe(false);
    expect(isAIFixURLValid('https://example.com/fixdefect')).toBe(false);
    expect(isAIFixURLValid('http://example.com/fixdefect?id={defect_id}')).toBe(false);
    expect(isAIFixURLValid(`${validURL}&legacy={defect_id}`)).toBe(false);
  });

  it('replaces the single placeholder without reconstructing the URL', () => {
    expect(buildAIFixURL(validURL, 9182)).toBe(
      'https://starmind-dev.sicarrier.com/shield-fix/fixdefect?id=9182'
    );
  });
});
