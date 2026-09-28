-- Weight of the Codohue relevance score in the mixed feed:
--
--   score = local score + provider_score * recommendation_weight
--
-- It was the literal 20 in the feed package, picked when Codohue mapped relevance
-- through a serve-time x/(x+5) curve that saturated near 0.99, so the term topped
-- out around 20 — level with recency_scale and relationship_bonus. Codohue
-- v0.12.0 replaced that curve with a clamped cosine, which changed the scale of
-- the value underneath this weight without anything here noticing. It moves here
-- so it can be re-set against the observed distribution without a redeploy.
--
-- Same bound as the other ranking weights. 0 is allowed: it removes the score
-- from ranking and leaves only the rank bonus, which is a legitimate way to
-- switch CF influence off while keeping the recommendations in the candidate set.
ALTER TABLE settings.feed
    ADD COLUMN recommendation_weight DOUBLE PRECISION NOT NULL DEFAULT 20
        CHECK (recommendation_weight >= 0 AND recommendation_weight <= 1000);
