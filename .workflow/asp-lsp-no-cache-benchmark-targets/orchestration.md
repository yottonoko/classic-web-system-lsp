# Orchestration

1. Establish no-cache baseline for each target operation using serial filtered huge runs.
2. Run packet investigations in parallel where possible:
   - P1: parser/scanner profile and low-risk optimizations.
   - P2: virtual document construction profile and low-risk optimizations.
   - P3: VBScript FromText/symbol/analyze profile and low-risk optimizations.
   - P4: verification hazards and benchmark command matrix.
3. Integrate only changes that improve uncached work, not cache reuse.
4. Run core tests/typecheck after each meaningful implementation group.
5. Run final target operation benchmark matrix with caches disabled.
6. Commit only task-related files after verification.
