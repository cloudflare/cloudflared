/* Copyright 2013 Google Inc. All Rights Reserved.

   Distributed under MIT license.
   See file LICENSE for detail or copy at https://opensource.org/licenses/MIT
*/

/* Functions for clustering similar histograms together. */

#include "./enc/cluster.h"

#include <brotli/types.h>
#include "./enc/bit_cost.h"  /* BrotliPopulationCost */
#include "./enc/fast_log.h"
#include "./enc/histogram.h"
#include "./enc/memory.h"
#include "./enc/port.h"

#if defined(__cplusplus) || defined(c_plusplus)
extern "C" {
#endif

static BROTLI_INLINE BROTLI_BOOL HistogramPairIsLess(
    const HistogramPair* p1, const HistogramPair* p2) {
  if (p1->cost_diff != p2->cost_diff) {
    return TO_BROTLI_BOOL(p1->cost_diff > p2->cost_diff);
  }
  return TO_BROTLI_BOOL((p1->idx2 - p1->idx1) > (p2->idx2 - p2->idx1));
}

/* Returns entropy reduction of the context map when we combine two clusters. */
static BROTLI_INLINE double ClusterCostDiff(size_t size_a, size_t size_b) {
  size_t size_c = size_a + size_b;
  return (double)size_a * FastLog2(size_a) +
    (double)size_b * FastLog2(size_b) -
    (double)size_c * FastLog2(size_c);
}

#define CODE(X) X

#define FN(X) X ## Literal
#include "./enc/cluster_inc.h"  /* NOLINT(build/include) */
#undef FN

#define FN(X) X ## Command
#include "./enc/cluster_inc.h"  /* NOLINT(build/include) */
#undef FN

#define FN(X) X ## Distance
#include "./enc/cluster_inc.h"  /* NOLINT(build/include) */
#undef FN

#undef CODE

#if defined(__cplusplus) || defined(c_plusplus)
}  /* extern "C" */
#endif
