/** The label for a thumbnail job's state, with each key spelled out so unused keys can be found. */
export function thumbnailJobStateLabel(t, state) {
  switch (state) {
    case 'queued':
      return t('thumbnailJobs.state.queued');
    case 'running':
      return t('thumbnailJobs.state.running');
    case 'done':
      return t('thumbnailJobs.state.done');
    case 'cancelled':
      return t('thumbnailJobs.state.cancelled');
    case 'failed':
      return t('thumbnailJobs.state.failed');
    default:
      return state;
  }
}
