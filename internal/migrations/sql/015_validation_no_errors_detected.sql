-- The playtest outcome formerly stored as 'passed' claimed more than the loop
-- had shown: it meant only that none of eight error substrings appeared in the
-- console. Rename the stored value to what was actually established so existing
-- runs stay readable under the new vocabulary rather than rendering as an
-- unknown outcome.
UPDATE runs SET validation = 'no_errors_detected' WHERE validation = 'passed';
